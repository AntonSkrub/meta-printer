//nolint:testpackage // Tests cover unexported PDF/text helper functions.
package filter

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

var testMeta = &Metadata{
	Filename:  "report.pdf",
	Filepath:  "/home/user/Documents/report.pdf",
	PrintTime: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
}

// ---- pdfString -------------------------------------------------------

func TestPDFString_ASCII(t *testing.T) {
	t.Parallel()

	got := pdfString("hello world")
	if got != "(hello world)" {
		t.Errorf("got %q, want %q", got, "(hello world)")
	}
}

func TestPDFString_EscapeParens(t *testing.T) {
	t.Parallel()

	got := pdfString("a(b)c")
	want := `(a\(b\)c)`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPDFString_EscapeBackslash(t *testing.T) {
	t.Parallel()

	got := pdfString(`a\b`)
	want := `(a\\b)`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPDFString_NonASCII(t *testing.T) {
	t.Parallel()

	// Characters > 0x7E and ≤ 0xFF must be octal-escaped.
	got := pdfString("caf\u00e9") // é = U+00E9
	if !strings.Contains(got, `\351`) {
		t.Errorf("expected octal escape for é, got %q", got)
	}
	// Characters outside Latin-1 must be replaced with '_'.
	got2 := pdfString("日本語") //nolint:gosmopolitan // Intentional Japanese test data.
	if !strings.HasPrefix(got2, "(") || !strings.HasSuffix(got2, ")") {
		t.Errorf("expected PDF string literal, got %q", got2)
	}
	if strings.Contains(got2, "日") { //nolint:gosmopolitan // Intentional Unicode test character.
		t.Errorf("expected non-representable chars to be replaced, got %q", got2)
	}
}

// ---- buildHeaderPDF -------------------------------------------------

func TestBuildHeaderPDF_ContainsMetadata(t *testing.T) {
	t.Parallel()

	data := buildHeaderPDF(testMeta)
	for _, want := range []string{"report.pdf", "/home/user", "2024-06-01", "BT", "ET", "%PDF-", "%%EOF"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("header PDF missing %q", want)
		}
	}
}

func TestBuildHeaderPDF_WrapsLongValues(t *testing.T) {
	t.Parallel()

	data := buildHeaderPDF(&Metadata{Filepath: strings.Repeat("a", 100)})
	if !bytes.Contains(data, []byte(strings.Repeat("a", 84))) ||
		!bytes.Contains(data, []byte(strings.Repeat("a", 16))) {
		t.Error("long path was not wrapped")
	}
}

// ---- prependPostScript -----------------------------------------------

func TestPrependPostScript(t *testing.T) {
	t.Parallel()

	original := "%!PS-Adobe-3.0\n%%Pages: 1\n"
	var out bytes.Buffer
	if err := prependPostScript(testMeta, strings.NewReader(original), &out); err != nil {
		t.Fatalf("prependPostScript: %v", err)
	}
	result := out.String()
	if !strings.Contains(result, "showpage") {
		t.Error("PostScript output missing showpage for cover page")
	}
	if !strings.Contains(result, original) {
		t.Error("PostScript output missing original document content")
	}
}

// ---- Prepend dispatcher ---------------------------------------------

func TestPrepend_UnsupportedType(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := Prepend("application/octet-stream", testMeta, strings.NewReader("data"), &out)
	if err == nil {
		t.Fatal("expected an error for an unsupported content type")
	}
}

func TestPrepend_PSType(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := Prepend("application/postscript", testMeta, strings.NewReader("%!PS\n"), &out)
	if err != nil {
		t.Fatalf("Prepend: %v", err)
	}
	if !strings.Contains(out.String(), "%!PS") {
		t.Error("PostScript original missing from output")
	}
}

func TestPrepend_PDFType(t *testing.T) {
	t.Parallel()

	original := buildTextPDF([]string{"original"}, 595, 842, 100)

	var out bytes.Buffer
	if err := Prepend("application/pdf", testMeta, bytes.NewReader(original), &out); err != nil {
		t.Fatalf("Prepend PDF: %v", err)
	}

	result := out.Bytes()
	if !bytes.HasPrefix(result, []byte("%PDF-")) {
		t.Error("merged PDF output does not start with %PDF-")
	}
	count, err := api.PageCount(bytes.NewReader(result), model.NewDefaultConfiguration())
	if err != nil || count != 1 {
		t.Errorf("output page count = %d, error = %v; want one page", count, err)
	}
}

//nolint:paralleltest // pdfcpu's config directory is process-global.
func TestPrependPDF_OnlyFirstPage(t *testing.T) {
	api.DisableConfigDir()
	first := buildTextPDF([]string{"original first"}, 595, 842, 100)
	second := buildTextPDF([]string{"original second"}, 595, 842, 100)
	var original bytes.Buffer
	if err := api.MergeRaw([]io.ReadSeeker{bytes.NewReader(first), bytes.NewReader(second)}, &original, false, nil); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Prepend("application/pdf", testMeta, bytes.NewReader(original.Bytes()), &out); err != nil {
		t.Fatal(err)
	}
	count, err := api.PageCount(bytes.NewReader(out.Bytes()), model.NewDefaultConfiguration())
	if err != nil || count != 2 {
		t.Fatalf("output page count = %d, error = %v; want two pages", count, err)
	}
	contents := make(map[int][]byte)
	err = api.ExtractContent(bytes.NewReader(out.Bytes()), nil, func(r io.Reader, page int) error {
		var readErr error
		contents[page], readErr = io.ReadAll(r)
		return readErr
	}, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(contents[1], []byte("original first")) || !bytes.Contains(contents[2], []byte("original second")) {
		t.Fatal("original page content was not preserved")
	}
	if bytes.Contains(contents[2], []byte(" Do")) {
		t.Error("second page unexpectedly contains a stamp")
	}
	if !bytes.Contains(contents[1], []byte(" Do")) {
		t.Error("first page does not contain a stamp")
	}
}

//nolint:paralleltest // pdfcpu's config directory is process-global.
func TestPrependPDF_ExtractedText(t *testing.T) {
	pdftotext, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext is not installed")
	}
	api.DisableConfigDir()
	first := buildTextPDF([]string{"original first"}, 595, 842, 100)
	second := buildTextPDF([]string{"original second"}, 595, 842, 100)
	var original bytes.Buffer
	if err := api.MergeRaw([]io.ReadSeeker{bytes.NewReader(first), bytes.NewReader(second)}, &original, false, nil); err != nil {
		t.Fatal(err)
	}
	meta := &Metadata{Filename: "test%path (1).pdf", Filepath: "/home/user/test%path (1).pdf", PrintTime: testMeta.PrintTime}
	var result bytes.Buffer
	if err := Prepend("application/pdf", meta, bytes.NewReader(original.Bytes()), &result); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "output.pdf")
	textPath := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(pdfPath, result.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(pdftotext, "-layout", pdfPath, textPath) // #nosec G204 -- executable is resolved with LookPath; paths are test-owned temp files.
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pdftotext: %v: %s", err, output)
	}
	text, err := os.ReadFile(textPath) // #nosec G304 -- textPath is created under t.TempDir().
	if err != nil {
		t.Fatal(err)
	}
	pages := strings.Split(string(text), "\f")
	if len(pages) < 2 {
		t.Fatalf("expected two pages, got %q", text)
	}
	for _, value := range []string{"original first", meta.Filename, meta.Filepath, "2024-06-01"} {
		if !strings.Contains(pages[0], value) {
			t.Errorf("first page missing %q in %q", value, pages[0])
		}
	}
	if !strings.Contains(pages[1], "original second") || strings.Contains(pages[1], meta.Filename) {
		t.Errorf("unexpected second page: %q", pages[1])
	}
}

func TestPrepend_StripsMIMEParameters(t *testing.T) {
	t.Parallel()

	original := buildTextPDF([]string{"original"}, 595, 842, 100)
	var out bytes.Buffer
	err := Prepend("application/pdf; version=1", testMeta, bytes.NewReader(original), &out)
	if err != nil {
		t.Fatalf("Prepend: %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Error("expected converted output to be a PDF")
	}
}
