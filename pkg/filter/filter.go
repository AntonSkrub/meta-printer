// Package filter injects document-metadata (filename, filepath, print time)
// into print jobs intercepted by the MetaPrinter CUPS filter.
//
// Supported input MIME types are PDF and PostScript. PDF metadata is added on
// a cover page, while PostScript jobs receive a PostScript cover page.
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
		return fmt.Errorf("filter: unsupported content type %q", contentType)
	}
}

// headerLines returns the metadata lines used in text-based headers.
func headerLines(m *Metadata) []string {
	return []string{
		"========================================",
		"  Document Metadata",
		"========================================",
		"  Filename : " + m.Filename,
		"  Path     : " + m.Filepath,
		"  Printed  : " + m.PrintTime.Format("2006-01-02 15:04:05 MST"),
		"========================================",
		"",
	}
}
