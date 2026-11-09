package filter

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// prependPDF overlays metadata on the first page without changing the source.
func prependPDF(m *Metadata, r io.Reader, w io.Writer) error {
	api.DisableConfigDir()

	original, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("pdf: read input: %w", err)
	}

	header := buildHeaderPDF(m)
	stamp, err := api.PDFWatermarkForReadSeeker(bytes.NewReader(header), 1,
		"pos:tl, off:18 -18, scale:0.92 rel, rot:0", true, false, types.POINTS)
	if err != nil {
		return fmt.Errorf("pdf: create header: %w", err)
	}
	if err := api.AddWatermarks(bytes.NewReader(original), w, []string{"1"}, stamp, nil); err != nil {
		return fmt.Errorf("pdf: add header: %w", err)
	}
	return nil
}

// buildHeaderPDF produces a small PDF stamp, rather than a full-size page.
// The stamp is placed over the existing page content, not added as a page.
func buildHeaderPDF(m *Metadata) []byte {
	const maxLineRunes = 90
	values := []string{
		"Filename: " + m.Filename,
		"Path: " + m.Filepath,
		"Printed: " + m.PrintTime.Format("2006-01-02 15:04:05 MST"),
	}
	var lines []string
	for _, value := range values {
		value = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(value)
		runes := []rune(value)
		for len(runes) > maxLineRunes {
			lines = append(lines, string(runes[:maxLineRunes]))
			runes = runes[maxLineRunes:]
		}
		lines = append(lines, string(runes))
	}

	return buildTextPDF(lines, 550, len(lines)*11+4, 0)
}

func buildTextPDF(lines []string, width, height, topInset int) []byte {
	var content strings.Builder
	content.WriteString("BT\n/F1 8 Tf\n")
	for i, line := range lines {
		fmt.Fprintf(&content, "1 0 0 1 0 %d Tm\n%s Tj\n", height-topInset-10-i*11, pdfString(line))
	}
	content.WriteString("ET")
	stream := content.String()

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	var off [6]int
	off[1] = b.Len()
	b.WriteString("1 0 obj\n<</Type /Catalog /Pages 2 0 R>>\nendobj\n")
	off[2] = b.Len()
	b.WriteString("2 0 obj\n<</Type /Pages /Kids [3 0 R] /Count 1>>\nendobj\n")
	off[3] = b.Len()
	fmt.Fprintf(&b, "3 0 obj\n<</Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Contents 4 0 R /Resources <</Font <</F1 5 0 R>>>>>>\nendobj\n", width, height)
	off[4] = b.Len()
	fmt.Fprintf(&b, "4 0 obj\n<</Length %d>>\nstream\n%s\nendstream\nendobj\n", len(stream), stream)
	off[5] = b.Len()
	b.WriteString("5 0 obj\n<</Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding>>\nendobj\n")
	xrefStart := b.Len()
	b.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", off[i])
	}
	fmt.Fprintf(&b, "trailer\n<</Size 6 /Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", xrefStart)
	return b.Bytes()
}

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
			b.WriteByte('_')
		}
	}
	b.WriteByte(')')
	return b.String()
}
