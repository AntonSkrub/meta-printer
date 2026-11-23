// Package filter injects document metadata (filename and filepath)
// into print jobs intercepted by the MetaPrinter CUPS filter.
//
// Supported input MIME types are PDF and PostScript. PDF metadata is overlaid
// on the first page, while PostScript jobs receive a cover page.
package filter

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"
)

// Metadata holds the information printed with the document.
type Metadata struct {
	Filename  string
	Filepath  string
	PrintTime time.Time
}

// Prepend detects the current stream format and adds metadata to the document.
// contentType describes the original CUPS job and may differ after conversion.
func Prepend(contentType string, m *Metadata, r io.Reader, w io.Writer) error {
	input := bufio.NewReaderSize(r, 1024)
	header, err := input.Peek(5)
	if err != nil && err != io.EOF {
		return fmt.Errorf("filter: read input signature: %w", err)
	}

	if bytes.HasPrefix(header, []byte("%!")) {
		return prependPostScript(m, input, w)
	}
	if bytes.HasPrefix(header, []byte("%PDF-")) {
		return prependPDF(m, input, w)
	}

	// PDF headers may appear within the first 1024 bytes.
	header, err = input.Peek(1024)
	if err != nil && err != io.EOF {
		return fmt.Errorf("filter: read input signature: %w", err)
	}
	if bytes.Contains(header, []byte("%PDF-")) {
		return prependPDF(m, input, w)
	}
	return fmt.Errorf("filter: unsupported or unrecognized input format (CUPS content type %q)", contentType)
}

// headerLines returns the metadata lines used in text-based headers.
func headerLines(m *Metadata) []string {
	lines := []string{
		"========================================",
		"  Document Metadata",
		"========================================",
	}
	for _, value := range []string{"  Filename : " + m.Filename, "  Path     : " + m.Filepath} {
		lines = append(lines, wrapHeaderLine(value, 35)...)
	}
	return append(lines, "========================================", "")
}

func wrapHeaderLine(value string, maxRunes int) []string {
	value = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(value)
	runes := []rune(value)
	var lines []string
	for len(runes) > maxRunes {
		lines = append(lines, string(runes[:maxRunes]))
		runes = runes[maxRunes:]
	}
	return append(lines, string(runes))
}
