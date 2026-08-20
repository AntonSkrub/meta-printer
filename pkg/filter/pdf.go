package filter

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// prependPDF creates a single-page PDF cover containing m's metadata, merges
// it with the original PDF from r, and writes the result to w.
func prependPDF(m *Metadata, r io.Reader, w io.Writer) error {
	original, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("pdf: read input: %w", err)
	}

	cover := buildCoverPagePDF(m)

	coverRS := bytes.NewReader(cover)
	originalRS := bytes.NewReader(original)

	if err := api.MergeRaw([]io.ReadSeeker{coverRS, originalRS}, w, false, nil); err != nil {
		return fmt.Errorf("pdf: merge: %w", err)
	}
	return nil
}

// buildCoverPagePDF creates a minimal, valid single-page PDF containing the
// metadata lines. It uses only the standard Type1 Helvetica font (no external
// resources required).
func buildCoverPagePDF(m *Metadata) []byte {
	content := buildContentStream(m)

	var b bytes.Buffer
	write := func(s string) { b.WriteString(s) }

	// PDF binary marker: four bytes > 127 indicate a binary file.
	write("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")

	off := [7]int{} // offsets for objects 1–6 (index 0 unused)

	// Object 1 – Catalog
	off[1] = b.Len()
	write("1 0 obj\n<</Type /Catalog /Pages 2 0 R>>\nendobj\n")

	// Object 2 – Pages
	off[2] = b.Len()
	write("2 0 obj\n<</Type /Pages /Kids [3 0 R] /Count 1>>\nendobj\n")

	// Object 3 – Page
	off[3] = b.Len()
	write("3 0 obj\n<</Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
		" /Contents 4 0 R /Resources <</Font <</F1 5 0 R>>>>>>\nendobj\n")

	// Object 4 – Content stream
	// Length counts only the stream data bytes (not the EOL before endstream).
	off[4] = b.Len()
	fmt.Fprintf(&b, "4 0 obj\n<</Length %d>>\nstream\n%s\nendstream\nendobj\n",
		len(content), content)

	// Object 5 – Helvetica font
	off[5] = b.Len()
	write("5 0 obj\n<</Type /Font /Subtype /Type1 /BaseFont /Helvetica" +
		" /Encoding /WinAnsiEncoding>>\nendobj\n")

	// Cross-reference table
	xrefStart := b.Len()
	write("xref\n0 6\n")
	write("0000000000 65535 f \n") // free-object entry, exactly 20 bytes
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", off[i]) // exactly 20 bytes
	}

	// Trailer
	fmt.Fprintf(&b, "trailer\n<</Size 6 /Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n",
		xrefStart)

	return b.Bytes()
}

// buildContentStream returns the PDF content-stream commands that render the
// metadata text on the cover page. The returned string must NOT end with a
// newline (the caller adds the required EOL separator before endstream).
//
// Each line uses "1 0 0 1 x y Tm" (text matrix) for absolute positioning so
// that individual lines are independent of each other.
func buildContentStream(m *Metadata) string {
	type line struct {
		text string
		size float64
		y    float64
	}
	lines := []line{
		{"Document Metadata", 18, 720},
		{"Filename : " + m.Filename, 12, 680},
		{"Path     : " + m.Filepath, 12, 658},
		{"Printed  : " + m.PrintTime.Format("2006-01-02 15:04:05 MST"), 12, 636},
	}

	var sb strings.Builder
	sb.WriteString("BT\n")
	for _, l := range lines {
		// "1 0 0 1 x y Tm" sets the text matrix to absolute coordinates (x, y).
		fmt.Fprintf(&sb, "/F1 %.0f Tf\n1 0 0 1 50 %.0f Tm\n%s Tj\n",
			l.size, l.y, pdfString(l.text))
	}
	sb.WriteString("ET")
	return sb.String()
}

// pdfString encodes s as a PDF literal string (enclosed in parentheses).
// Characters outside printable ASCII are encoded as octal escapes so they
// work with the standard WinAnsiEncoding.
func pdfString(s string) string {
	var b strings.Builder
	b.WriteByte('(')
	for _, r := range s {
		switch {
		case r == '(':
			b.WriteString(`\(`)
		case r == ')':
			b.WriteString(`\)`)
		case r == '\\':
			b.WriteString(`\\`)
		case r >= 0x20 && r <= 0x7E:
			b.WriteRune(r)
		case r > 0x7E && r <= 0xFF:
			fmt.Fprintf(&b, `\%03o`, r)
		default:
			b.WriteByte('_') // replace non-representable characters
		}
	}
	b.WriteByte(')')
	return b.String()
}
