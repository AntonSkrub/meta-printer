package filter

import (
	"bytes"
	"context"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// requireSoffice skips the calling test unless a real, UNO-capable
// LibreOffice/python3 toolchain is available; these are integration tests
// that exercise the actual conversion.
func requireSoffice(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skip("soffice not found in PATH, skipping LibreOffice integration test")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found in PATH, skipping LibreOffice integration test")
	}
}

func TestOfficeExtensions_KnownTypes(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
		"application/vnd.oasis.opendocument.text":                                 "odt",
		"application/msword":                                                     "doc",
		"application/rtf":                                                       "rtf",
		"text/rtf":                                                              "rtf",
		"text/plain":                                                            "txt",
	}
	for ct, wantExt := range cases {
		if got, ok := officeExtensions[ct]; !ok || got != wantExt {
			t.Errorf("officeExtensions[%q] = (%q, %v), want (%q, true)", ct, got, ok, wantExt)
		}
	}
}

func TestPrependOfficeHeader_MissingSoffice(t *testing.T) {
	t.Parallel()

	origSoffice := sofficeBinary
	sofficeBinary = "metaprinter-soffice-does-not-exist"
	t.Cleanup(func() { sofficeBinary = origSoffice })

	var out bytes.Buffer
	err := prependOfficeHeader("txt", testMeta, strings.NewReader("hi"), &out)
	if err == nil {
		t.Fatal("expected an error when soffice is not found in PATH")
	}
	if !strings.Contains(err.Error(), "soffice") {
		t.Errorf("error should mention the missing soffice binary, got %q", err)
	}
}

func TestPrependOfficeHeader_MissingPython(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skip("soffice not found in PATH, skipping")
	}

	origPython := pythonBinary
	pythonBinary = "metaprinter-python-does-not-exist"
	t.Cleanup(func() { pythonBinary = origPython })

	var out bytes.Buffer
	err := prependOfficeHeader("txt", testMeta, strings.NewReader("hi"), &out)
	if err == nil {
		t.Fatal("expected an error when python3 is not found in PATH")
	}
	if !strings.Contains(err.Error(), "python3") {
		t.Errorf("error should mention the missing python3 binary, got %q", err)
	}
}

func TestFreeTCPPort_ReturnsUsablePort(t *testing.T) {
	t.Parallel()

	port, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	if port <= 0 {
		t.Fatalf("expected a positive port, got %d", port)
	}
}

func TestWaitForPort_SucceedsOnListeningPort(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // Listener always returns *net.TCPAddr for tcp.

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := waitForPort(ctx, port); err != nil {
		t.Fatalf("waitForPort: %v", err)
	}
}

func TestWaitForPort_TimesOutWhenNothingListens(t *testing.T) {
	t.Parallel()

	// Grab a free port and immediately release it, so nothing is listening.
	port, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if err := waitForPort(ctx, port); err == nil {
		t.Fatal("expected waitForPort to time out when nothing listens on the port")
	}
}
