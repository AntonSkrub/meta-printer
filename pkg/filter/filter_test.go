//nolint:testpackage // Tests cover unexported PDF/text helper functions.
package filter

import (
	"bytes"
	"strings"
	"testing"
	"time"
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

// ---- buildContentStream / buildCoverPagePDF --------------------------

func TestBuildContentStream_ContainsMetadata(t *testing.T) {
	t.Parallel()

	cs := buildContentStream(testMeta)
	for _, want := range []string{"report.pdf", "/home/user", "BT", "ET"} {
		if !strings.Contains(cs, want) {
			t.Errorf("content stream missing %q", want)
		}
	}
}

func TestBuildCoverPagePDF_ValidHeader(t *testing.T) {
	t.Parallel()

	data := buildCoverPagePDF(testMeta)

	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Error("output does not start with %PDF-")
	}
	if !bytes.Contains(data, []byte("%%EOF")) {
		t.Error("output does not contain PDF end-of-file marker")
	}
}

// ---- prependText -----------------------------------------------------

func TestPrependText(t *testing.T) {
	t.Parallel()

	original := "Hello, world!\n"
	var out bytes.Buffer
	if err := prependText(testMeta, strings.NewReader(original), &out); err != nil {
		t.Fatalf("prependText: %v", err)
	}
	result := out.String()
	for _, want := range []string{"report.pdf", "/home/user", "2024-06-01", original} {
		if !strings.Contains(result, want) {
			t.Errorf("output missing %q", want)
		}
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

func TestPrepend_TextType(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := Prepend("text/plain", testMeta, strings.NewReader("body\n"), &out); err != nil {
		t.Fatalf("Prepend: %v", err)
	}
	if !strings.Contains(out.String(), "body") {
		t.Error("original body missing from output")
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

	// Use our own cover-page PDF as a stand-in for the "original document"
	// so we don't need an external file.
	original := buildCoverPagePDF(testMeta)

	var out bytes.Buffer
	if err := Prepend("application/pdf", testMeta, bytes.NewReader(original), &out); err != nil {
		t.Fatalf("Prepend PDF: %v", err)
	}

	result := out.Bytes()
	if !bytes.HasPrefix(result, []byte("%PDF-")) {
		t.Error("merged PDF output does not start with %PDF-")
	}
	// Merged output must be larger than a single cover page.
	if len(result) <= len(original) {
		t.Errorf("merged PDF (%d bytes) is not larger than single page (%d bytes)",
			len(result), len(original))
	}
}

func TestPrepend_StripsMIMEParameters(t *testing.T) {
	t.Parallel()

	// MIME type with charset parameter must still route to text handler.
	var out bytes.Buffer
	err := Prepend("text/plain; charset=utf-8", testMeta, strings.NewReader("hi\n"), &out)
	if err != nil {
		t.Fatalf("Prepend: %v", err)
	}
	if !strings.Contains(out.String(), "hi") {
		t.Error("original body missing from output")
	}
}
