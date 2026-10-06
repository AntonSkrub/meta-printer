package filter

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// officeExtensions maps normalized MIME types recognised as Writer-compatible
// office/text sources to the file extension LibreOffice needs to correctly
// detect the source format.
var officeExtensions = map[string]string{
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
	"application/vnd.oasis.opendocument.text":                                 "odt",
	"application/msword":                                                     "doc",
	"application/rtf":                                                        "rtf",
	"text/rtf":                                                               "rtf",
	"text/plain":                                                             "txt",
}

// sofficeBinary and pythonBinary name the executables used to drive the
// headless LibreOffice conversion. They are package-level variables so tests
// can point them at stand-ins without touching PATH.
var (
	sofficeBinary = "soffice"
	pythonBinary  = "python3"
)

// officeConvertTimeout bounds the whole headless-conversion round trip
// (soffice startup, UNO connection, header edit, PDF export).
const officeConvertTimeout = 90 * time.Second

// prependOfficeHeader converts the office/text document read from r into a
// print-ready PDF whose Writer page-style header carries the metadata. It
// drives a throwaway, isolated headless LibreOffice instance through a
// Python/UNO helper script, so the original file is never modified.
func prependOfficeHeader(ext string, m *Metadata, r io.Reader, w io.Writer) error {
	if _, err := exec.LookPath(sofficeBinary); err != nil {
		return fmt.Errorf("office: %s not found in PATH: install LibreOffice", sofficeBinary)
	}
	if _, err := exec.LookPath(pythonBinary); err != nil {
		return fmt.Errorf("office: %s not found in PATH: install python3 with LibreOffice UNO bindings", pythonBinary)
	}

	workDir, err := os.MkdirTemp("", "metaprinter-office-*")
	if err != nil {
		return fmt.Errorf("office: create workdir: %w", err)
	}
	defer os.RemoveAll(workDir)

	inputPath := filepath.Join(workDir, "input."+ext)
	if err := writeFile(inputPath, r); err != nil {
		return fmt.Errorf("office: write input: %w", err)
	}

	outputPath := filepath.Join(workDir, "output.pdf")
	scriptPath := filepath.Join(workDir, "set_header.py")
	if err := os.WriteFile(scriptPath, []byte(officeHeaderScript), 0o600); err != nil {
		return fmt.Errorf("office: write helper script: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), officeConvertTimeout)
	defer cancel()

	if err := convertWithLibreOffice(ctx, workDir, scriptPath, inputPath, outputPath, headerLines(m)); err != nil {
		return fmt.Errorf("office: convert: %w", err)
	}

	output, err := os.Open(outputPath)
	if err != nil {
		return fmt.Errorf("office: open converted output: %w", err)
	}
	defer output.Close()

	if _, err := io.Copy(w, output); err != nil {
		return fmt.Errorf("office: write converted output: %w", err)
	}
	return nil
}

// writeFile drains r into a new file at path.
func writeFile(path string, r io.Reader) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// convertWithLibreOffice starts an isolated headless LibreOffice instance,
// drives it through the Python/UNO helper script to set the page-style
// header and export a PDF, then tears the instance down.
func convertWithLibreOffice(ctx context.Context, workDir, scriptPath, inputPath, outputPath string, header []string) error {
	port, err := freeTCPPort()
	if err != nil {
		return fmt.Errorf("find free port: %w", err)
	}

	// Each job gets its own profile so concurrent print jobs never contend
	// for the same LibreOffice user-profile lock.
	profileDir := filepath.Join(workDir, "profile")
	soffice := exec.CommandContext(ctx, sofficeBinary,
		"--headless", "--invisible", "--nocrashreport", "--nodefault",
		"--norestore", "--nologo", "--nofirststartwizard",
		"-env:UserInstallation=file://"+filepath.ToSlash(profileDir),
		fmt.Sprintf("--accept=socket,host=localhost,port=%d;urp;", port),
	)
	if err := soffice.Start(); err != nil {
		return fmt.Errorf("start soffice: %w", err)
	}
	defer func() {
		_ = soffice.Process.Kill()
		_ = soffice.Wait()
	}()

	if err := waitForPort(ctx, port); err != nil {
		return fmt.Errorf("wait for soffice listener: %w", err)
	}

	pyArgs := append([]string{scriptPath, fmt.Sprintf("%d", port), inputPath, outputPath}, header...)
	out, err := exec.CommandContext(ctx, pythonBinary, pyArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set_header.py: %w: %s", err, out)
	}
	return nil
}

// freeTCPPort returns a currently-unused TCP port on localhost.
func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", l.Addr())
	}
	return addr.Port, nil
}

// waitForPort blocks until a TCP connection to localhost:port succeeds or ctx
// is done.
func waitForPort(ctx context.Context, port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// officeHeaderScript is a Python/UNO helper run inside the LibreOffice
// process. It connects to the headless instance, opens the document, inserts
// the given header lines before any existing header content on every page
// style, and exports the result as PDF.
//
// Usage: set_header.py <uno-port> <input-path> <output-path> [header-line ...]
const officeHeaderScript = `#!/usr/bin/env python3
import sys
import time

import uno
from com.sun.star.beans import PropertyValue


def make_prop(name, value):
    prop = PropertyValue()
    prop.Name = name
    prop.Value = value
    return prop


def connect(port, attempts=60, delay=0.5):
    local_ctx = uno.getComponentContext()
    resolver = local_ctx.ServiceManager.createInstanceWithContext(
        "com.sun.star.bridge.UnoUrlResolver", local_ctx)
    url = "uno:socket,host=localhost,port=%d;urp;StarOffice.ComponentContext" % port
    last_err = None
    for _ in range(attempts):
        try:
            return resolver.resolve(url)
        except Exception as exc:  # retry until soffice accepts the connection
            last_err = exc
            time.sleep(delay)
    raise RuntimeError("could not connect to soffice: %s" % last_err)


def set_header(doc, header_lines):
    styles = doc.StyleFamilies.getByName("PageStyles")
    for i in range(styles.Count):
        style = styles.getByIndex(i)
        if not hasattr(style, "HeaderIsOn"):
            continue
        style.HeaderIsOn = True
        header_text = style.HeaderText
        cursor = header_text.createTextCursor()
        cursor.gotoStart(False)
        for line in header_lines:
            header_text.insertString(cursor, line, False)
            header_text.insertControlCharacter(
                cursor,
                uno.getConstantByName("com.sun.star.text.ControlCharacter.PARAGRAPH_BREAK"),
                False)


def main():
    port = int(sys.argv[1])
    input_path = sys.argv[2]
    output_path = sys.argv[3]
    header_lines = sys.argv[4:]

    ctx = connect(port)
    smgr = ctx.ServiceManager
    desktop = smgr.createInstanceWithContext("com.sun.star.frame.Desktop", ctx)

    in_url = uno.systemPathToFileUrl(input_path)
    out_url = uno.systemPathToFileUrl(output_path)

    doc = desktop.loadComponentFromURL(in_url, "_blank", 0, (make_prop("Hidden", True),))
    try:
        set_header(doc, header_lines)
        doc.storeToURL(out_url, (make_prop("FilterName", "writer_pdf_Export"),))
    finally:
        doc.close(False)


if __name__ == "__main__":
    main()
`
