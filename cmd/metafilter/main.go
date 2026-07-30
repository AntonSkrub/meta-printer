// metafilter is a CUPS print filter that prepends document metadata
// (filename, filepath, print timestamp) to every intercepted print job.
//
// CUPS invokes the filter as:
//
//	metafilter job-id user title copies options [filename]
//
// If filename is provided the input is read from that file; otherwise stdin
// is used. The processed document is written to stdout.
//
// The filter looks up richer metadata (original file path) from the metadata
// daemon's SQLite database at /var/lib/meta-printer/<user>.db, falling back
// gracefully to the job title when the database is unavailable.
//
// The CONTENT_TYPE environment variable (set by CUPS) determines the output
// format:
//   - application/pdf or application/vnd.cups-pdf → PDF with cover page
//   - application/postscript or application/vnd.cups-postscript → PostScript
//   - anything else (including text/plain) → plain text
package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/AntonSkrub/meta-printer/pkg/config"
	"github.com/AntonSkrub/meta-printer/pkg/db"
	"github.com/AntonSkrub/meta-printer/pkg/filter"
)

func main() {
	// CUPS passes exactly 5 or 6 positional arguments (plus argv[0]).
	if len(os.Args) < 6 {
		fmt.Fprintln(os.Stderr,
			"Usage: metafilter job-id user title copies options [filename]")
		os.Exit(1)
	}

	// argv[1] job-id, argv[2] user, argv[3] title, argv[4] copies,
	// argv[5] options, argv[6] (optional) filename.
	user := os.Args[2]
	title := os.Args[3]

	// Open input: file argument takes precedence over stdin.
	input, closeInput, err := openInput(os.Args)
	if err != nil {
		log.Fatalf("metafilter: open input: %v", err)
	}
	defer closeInput()

	meta := buildMetadata(user, title)

	// Determine content type from the CUPS environment variable.
	contentType := os.Getenv("CONTENT_TYPE")
	if contentType == "" {
		contentType = "application/pdf" // safe default
	}

	if err := filter.Prepend(contentType, meta, input, os.Stdout); err != nil {
		log.Fatalf("metafilter: prepend metadata: %v", err)
	}
}

// openInput returns a reader for the print-job content.
// If argv[6] is provided and non-empty, the file is opened; otherwise stdin
// is returned. The returned closer must be called when done.
func openInput(args []string) (io.Reader, func(), error) {
	if len(args) >= 7 && args[6] != "" {
		f, err := os.Open(args[6])
		if err != nil {
			return nil, func() {}, err
		}
		return f, func() { f.Close() }, nil
	}
	return os.Stdin, func() {}, nil
}

// buildMetadata constructs the Metadata struct for a print job.
// It first tries to look up the original file path from the daemon's database;
// if that fails (db unavailable or no matching record) it falls back to
// using the job title as both filename and path.
func buildMetadata(user, title string) *filter.Metadata {
	meta := &filter.Metadata{
		Filename:  filepath.Base(title),
		Filepath:  title,
		PrintTime: time.Now(),
	}

	cfg, err := config.Load()
	if err != nil {
		log.Printf("metafilter: config warning: %v (using defaults)", err)
	}

	store, err := db.New(filepath.Join(cfg.FilterDBDir, user+".db"))
	if err != nil {
		// Database not available – use job-title metadata only.
		return meta
	}
	defer store.Close()

	record, err := store.LookupByFilename(meta.Filename)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("metafilter: db lookup: %v", err)
		}
		return meta
	}

	// Enrich with the full path recorded by the daemon.
	meta.Filepath = record.Filepath

	if err := store.MarkPrinted(record.ID); err != nil {
		log.Printf("metafilter: mark printed: %v", err)
	}
	return meta
}
