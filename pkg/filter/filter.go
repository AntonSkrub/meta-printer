// Package filter injects document-metadata (filename, filepath, print time)
// into print jobs intercepted by the MetaPrinter CUPS filter.
//
// Supported input MIME types:
//   - application/pdf, application/vnd.cups-pdf → metadata overlaid on the
//     top margin of every page, original PDF pages preserved
//   - application/postscript, application/vnd.cups-postscript → PostScript
//     cover page prepended
//   - DOCX, ODT, DOC, RTF, plain text → converted to a print-ready PDF whose
//     Writer page-style header (Kopfzeile) carries the metadata, via a
//     headless LibreOffice instance; any existing header content is kept,
//     with the metadata inserted before it
//
// Any other MIME type is rejected with an error rather than silently
// treated as plain text, to avoid corrupting binary inputs.
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
		if ext, ok := officeExtensions[ct]; ok {
			return prependOfficeHeader(ext, m, r, w)
		}
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
