//nolint:testpackage // Tests cover unexported PDF/text helper functions.
package filter

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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

	data, _ := buildHeaderPDF(testMeta, 90)
	for _, want := range []string{"report.pdf", "/home/user", "BT", "ET", "%PDF-", "%%EOF"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("header PDF missing %q", want)
		}
	}
	if bytes.Contains(data, []byte("Printed:")) || bytes.Contains(data, []byte("2024-06-01")) {
		t.Error("header contains print time")
	}
}

func TestBuildHeaderPDF_WrapsLongValues(t *testing.T) {
	t.Parallel()

	data, _ := buildHeaderPDF(&Metadata{Filepath: strings.Repeat("a", 200)}, 90)
	if !bytes.Contains(data, []byte(strings.Repeat("a", 84))) ||
		!bytes.Contains(data, []byte(strings.Repeat("a", 90))) ||
		!bytes.Contains(data, []byte(strings.Repeat("a", 26))) {
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
	if strings.Contains(result, "Printed") || strings.Contains(result, "2024-06-01") {
		t.Error("PostScript cover contains print time")
	}
}

func TestPrependPostScriptWrapsLongPath(t *testing.T) {
	t.Parallel()
	meta := &Metadata{Filepath: strings.Repeat("x", 80)}
	var out bytes.Buffer
	if err := prependPostScript(meta, strings.NewReader("%!PS\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "(  Path     : "+strings.Repeat("x", 22)+") show") ||
		!strings.Contains(out.String(), "("+strings.Repeat("x", 35)+") show") ||
		!strings.Contains(out.String(), "("+strings.Repeat("x", 23)+") show") {
		t.Fatal("PostScript path was not wrapped")
	}
}

func TestUnavailablePathAppearsInHeaders(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path, psFirst, psContinuation string
	}{
		{"unavailable - ambiguous match", "unavailable - ambiguou", "s match"},
		{"unavailable - no match", "unavailable - no match", ""},
		{"unavailable - lookup error", "unavailable - lookup e", "rror"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			meta := &Metadata{Filename: "Sample PDF", Filepath: tc.path}
			pdf, _ := buildHeaderPDF(meta, 90)
			if !bytes.Contains(pdf, []byte("(Filename: Sample PDF)")) || !bytes.Contains(pdf, []byte("(Path: "+tc.path+")")) {
				t.Fatalf("PDF header missing fallback: %s", pdf)
			}
			ps, err := buildPSCoverPage(meta)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(ps, "(  Filename : Sample PDF) show") ||
				!strings.Contains(ps, "(  Path     : "+tc.psFirst+") show") ||
				(tc.psContinuation != "" && !strings.Contains(ps, "("+tc.psContinuation+") show")) {
				t.Fatalf("PostScript cover missing fallback: %s", ps)
			}
		})
	}
}

func TestPrependPostScriptRejectsOversizedHeader(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := prependPostScript(&Metadata{Filepath: strings.Repeat("x", 2000)}, strings.NewReader("%!PS\n"), &out); err == nil {
		t.Fatal("expected an error when the header cannot fit")
	}
	if out.Len() != 0 {
		t.Fatal("oversized header produced output")
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

//nolint:paralleltest // pdfcpu's config directory is process-global.
func TestPrepend_ConvertedPostScriptJob(t *testing.T) {
	api.DisableConfigDir()
	original := buildTextPDF([]string{"original content"}, 595, 842, 100)
	var out bytes.Buffer
	// Use a non-seekable reader to mirror input from the preceding CUPS filter.
	if err := Prepend("application/postscript", testMeta, struct{ io.Reader }{bytes.NewReader(original)}, &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Fatal("converted PostScript job must remain a PDF")
	}
	count, err := api.PageCount(bytes.NewReader(out.Bytes()), model.NewDefaultConfiguration())
	if err != nil || count != 1 {
		t.Fatalf("page count = %d, error = %v; want one page", count, err)
	}
	err = api.ExtractContent(bytes.NewReader(out.Bytes()), []string{"1"}, func(r io.Reader, _ int) error {
		content, readErr := io.ReadAll(r)
		if readErr != nil {
			return readErr
		}
		if !bytes.Contains(content, []byte("original content")) || !bytes.Contains(content, []byte(" Do")) {
			t.Errorf("first page must retain original content and include header stamp: %s", content)
		}
		return nil
	}, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
}

//nolint:paralleltest // pdfcpu's config directory is process-global.
func TestPrepend_ConvertedPostScriptUsesPDFPlacement(t *testing.T) {
	for _, page := range []struct {
		name          string
		width, height int
	}{
		{"A4", 595, 842},
		{"Letter", 612, 792},
	} {
		t.Run(page.name, func(t *testing.T) {
			original := buildTextPDF([]string{"original content"}, page.width, page.height, 100)
			var transforms []string
			for _, contentType := range []string{"application/pdf", "application/postscript"} {
				var output bytes.Buffer
				if err := Prepend(contentType, testMeta, bytes.NewReader(original), &output); err != nil {
					t.Fatal(err)
				}
				err := api.ExtractContent(bytes.NewReader(output.Bytes()), []string{"1"}, func(r io.Reader, _ int) error {
					content, err := io.ReadAll(r)
					if err != nil {
						return err
					}
					transform := regexp.MustCompile(`q [0-9.]+ 0\.00000 0\.00000 [0-9.]+ [0-9.]+ [0-9.]+ cm /\S+ gs /\S+ Do`).Find(content)
					if transform == nil {
						return errors.New("header transform not found")
					}
					transforms = append(transforms, string(transform))
					return nil
				}, model.NewDefaultConfiguration())
				if err != nil {
					t.Fatal(err)
				}
			}
			if transforms[0] != transforms[1] {
				t.Errorf("header placements differ: %q and %q", transforms[0], transforms[1])
			}
		})
	}
}

func TestPrepend_UsesActualPostScriptInput(t *testing.T) {
	t.Parallel()
	original := "%!PS-Adobe-3.0\n%%Pages: 1\n"
	var out bytes.Buffer
	if err := Prepend("application/pdf", testMeta, strings.NewReader(original), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "%!PS") || !strings.Contains(out.String(), original) ||
		!strings.Contains(out.String(), "Document Metadata") {
		t.Fatalf("expected PostScript metadata cover and original content: %q", out.String())
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("input failed") }

func TestPrepend_InvalidInput(t *testing.T) {
	t.Parallel()
	for _, input := range []io.Reader{strings.NewReader(""), strings.NewReader("not a document"), failingReader{}} {
		var out bytes.Buffer
		if err := Prepend("application/postscript", testMeta, input, &out); err == nil {
			t.Error("expected invalid or unreadable input to fail")
		}
		if out.Len() != 0 {
			t.Error("invalid input produced output")
		}
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

	// Check the rendered stamp's top edge against the first page's top edge.
	transform := regexp.MustCompile(`q ([0-9.]+) 0\.00000 0\.00000 [0-9.]+ ([0-9.]+) ([0-9.]+) cm /\S+ gs /\S+ Do`).FindSubmatch(contents[1])
	if transform == nil {
		t.Fatalf("header transform not found in first page: %s", contents[1])
	}
	scale, err := strconv.ParseFloat(string(transform[1]), 64)
	if err != nil {
		t.Fatal(err)
	}
	x, err := strconv.ParseFloat(string(transform[2]), 64)
	if err != nil {
		t.Fatal(err)
	}
	const pageWidth = 595.0
	headerWidth := math.Ceil(float64(len([]rune("Path: "+testMeta.Filepath))) * 4.8)
	if offset := pageWidth - (x + scale*headerWidth); math.Abs(offset-10*72/25.4) > 0.25 {
		t.Errorf("header right offset = %.2f points; want 10 mm", offset)
	}
	y, err := strconv.ParseFloat(string(transform[3]), 64)
	if err != nil {
		t.Fatal(err)
	}
	const pageHeight, headerHeight, wantOffset = 842.0, 26.0, 9.0
	if offset := pageHeight - (y + scale*headerHeight); math.Abs(offset-wantOffset) > 0.25 {
		t.Errorf("header top offset = %.2f points; want %.2f", offset, wantOffset)
	}
}

//nolint:paralleltest // pdfcpu's config directory is process-global.
func TestPrependPDF_NarrowPageWrapsWithinMargins(t *testing.T) {
	original := buildTextPDF([]string{"original"}, 200, 842, 100)
	meta := &Metadata{Filename: "file.pdf", Filepath: strings.Repeat("a", 160)}
	var out bytes.Buffer
	if err := Prepend("application/pdf", meta, bytes.NewReader(original), &out); err != nil {
		t.Fatal(err)
	}
	err := api.ExtractContent(bytes.NewReader(out.Bytes()), []string{"1"}, func(r io.Reader, _ int) error {
		content, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		transform := regexp.MustCompile(`q ([0-9.]+) 0\.00000 0\.00000 [0-9.]+ ([0-9.]+) ([0-9.]+) cm /\S+ gs /\S+ Do`).FindSubmatch(content)
		if transform == nil {
			t.Fatalf("header transform not found: %s", content)
		}
		x, err := strconv.ParseFloat(string(transform[2]), 64)
		if err != nil {
			return err
		}
		if x < 10*72/25.4-0.25 {
			t.Errorf("header extends into the left margin: x=%.2f", x)
		}
		return nil
	}, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
}

//nolint:paralleltest // pdfcpu's config directory is process-global.
func TestPrependPDF_RejectsOversizedHeader(t *testing.T) {
	original := buildTextPDF([]string{"original"}, 200, 200, 20)
	var out bytes.Buffer
	if err := Prepend("application/pdf", &Metadata{Filepath: strings.Repeat("x", 2000)}, bytes.NewReader(original), &out); err == nil {
		t.Fatal("expected an error when the header cannot fit")
	}
	if out.Len() != 0 {
		t.Fatal("oversized header produced output")
	}
}

//nolint:paralleltest // pdfcpu's config directory is process-global.
func TestPrependPDF_RotatedPageRightAlignment(t *testing.T) {
	original := buildTextPDF([]string{"original"}, 595, 842, 100)
	var rotated bytes.Buffer
	if err := api.Rotate(bytes.NewReader(original), &rotated, 90, []string{"1"}, model.NewDefaultConfiguration()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Prepend("application/pdf", testMeta, bytes.NewReader(rotated.Bytes()), &out); err != nil {
		t.Fatal(err)
	}
	err := api.ExtractContent(bytes.NewReader(out.Bytes()), []string{"1"}, func(r io.Reader, _ int) error {
		content, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		transform := regexp.MustCompile(`q ([0-9.]+) 0\.00000 0\.00000 [0-9.]+ ([0-9.]+) ([0-9.]+) cm /\S+ gs /\S+ Do`).FindSubmatch(content)
		if transform == nil {
			t.Fatalf("header transform not found: %s", content)
		}
		x, err := strconv.ParseFloat(string(transform[2]), 64)
		if err != nil {
			return err
		}
		width := math.Ceil(float64(len([]rune("Path: "+testMeta.Filepath))) * 4.8)
		if offset := 842 - x - width; math.Abs(offset-10*72/25.4) > 0.25 {
			t.Errorf("rotated page header right offset = %.2f points", offset)
		}
		return nil
	}, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
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
	for _, contentType := range []string{"application/pdf", "application/postscript"} {
		t.Run(contentType, func(t *testing.T) {
			var result bytes.Buffer
			if err := Prepend(contentType, meta, bytes.NewReader(original.Bytes()), &result); err != nil {
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
			for _, value := range []string{"original first", meta.Filename, meta.Filepath} {
				if !strings.Contains(pages[0], value) {
					t.Errorf("first page missing %q in %q", value, pages[0])
				}
				if strings.Contains(pages[0], "Printed:") || strings.Contains(pages[0], "2024-06-01") {
					t.Error("first page contains print time")
				}
			}
			if !strings.Contains(pages[1], "original second") || strings.Contains(pages[1], meta.Filename) {
				t.Errorf("unexpected second page: %q", pages[1])
			}
		})
	}
}

//nolint:paralleltest // pdfcpu's config directory is process-global.
func TestPrependPDF_ExtractedWrappedPath(t *testing.T) {
	pdftotext, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext is not installed")
	}
	original := buildTextPDF([]string{"original"}, 595, 842, 100)
	var out bytes.Buffer
	if err := Prepend("application/pdf", &Metadata{Filename: "file.pdf", Filepath: strings.Repeat("x", 150)}, bytes.NewReader(original), &out); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pdfPath, textPath := filepath.Join(dir, "wrapped.pdf"), filepath.Join(dir, "wrapped.txt")
	if err := os.WriteFile(pdfPath, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(pdftotext, "-layout", pdfPath, textPath) // #nosec G204 -- executable and files are trusted.
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pdftotext: %v: %s", err, output)
	}
	text, err := os.ReadFile(textPath) // #nosec G304 -- file is under t.TempDir().
	if err != nil {
		t.Fatal(err)
	}
	pattern := `Path: x{84}\s+x{66}`
	if !regexp.MustCompile(pattern).Match(text) {
		t.Fatalf("wrapped path not present in extracted text: %q", text)
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
