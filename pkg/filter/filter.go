// Package filter prepends document-metadata (filename, filepath, print time)
// to print jobs intercepted by the MetaPrinter CUPS filter.
//
// Supported input MIME types:
//   - application/pdf            → PDF cover page prepended and PDFs merged
//   - application/postscript     → PostScript header page prepended
//   - application/vnd.cups-pdf  → treated as PDF
//   - text/plain                 → plain-text header prepended
package filter

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Metadata holds the information that is printed on the cover page.
type Metadata struct {
	Filename  string
	Filepath  string
	PrintTime time.Time
}

// Prepend reads the document from r, prepends a metadata cover appropriate for
// contentType, and writes the result to w.
func Prepend(contentType string, m *Metadata, r io.Reader, w io.Writer) error {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	// Strip parameters (e.g. "application/pdf; charset=utf-8").
	if idx := strings.IndexByte(ct, ';'); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}

	switch ct {
	case "application/pdf", "application/vnd.cups-pdf":
		return prependPDF(m, r, w)
	case "application/postscript", "application/vnd.cups-postscript":
		return prependPostScript(m, r, w)
	default:
		// text/plain and any unrecognised type – prepend a plain-text header.
		return prependText(m, r, w)
	}
}

// headerLines returns the metadata lines used in text-based headers.
func headerLines(m *Metadata) []string {
	return []string{
		"========================================",
		"  Document Metadata",
		"========================================",
		fmt.Sprintf("  Filename : %s", m.Filename),
		fmt.Sprintf("  Path     : %s", m.Filepath),
		fmt.Sprintf("  Printed  : %s", m.PrintTime.Format("2006-01-02 15:04:05 MST")),
		"========================================",
		"",
	}
}
