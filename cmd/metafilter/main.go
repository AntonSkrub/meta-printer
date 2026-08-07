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
// daemon's SQLite database at /var/lib/meta-printer/<config.Get().DaemonDB.Name>.db, falling back
// gracefully to the job title when the database is unavailable.
//
// The CONTENT_TYPE environment variable (set by CUPS) determines the output
// format:
//   - application/pdf or application/vnd.cups-pdf → PDF with cover page
//   - application/postscript or application/vnd.cups-postscript → PostScript
//   - anything else (including text/plain) → plain text
package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/AntonSkrub/meta-printer/pkg/config"
	"github.com/AntonSkrub/meta-printer/pkg/database"
	"github.com/AntonSkrub/meta-printer/pkg/filter"
	"github.com/valentin-kaiser/go-core/apperror"
	"github.com/valentin-kaiser/go-core/flag"
	"github.com/valentin-kaiser/go-core/interruption"
	"github.com/valentin-kaiser/go-core/version"
)

func init() {
	defer interruption.Catch()
	apperror.ErrorHandler = func(err error, msg string) { //nolint:reassign
		log.Error().Err(err).Msg(msg)
	}
	config.Init()
}

func main() {
	defer interruption.Catch()

	if flag.Help {
		flag.PrintHelp()
		return
	}

	if flag.Version {
		log.Info().Str("version", version.String()).Msg("metafilter")
		fmt.Print(version.String())
		return
	}

	log.Info().Msgf("=== Meta-Printer filter %s ===", version.String())
	if flag.Debug {
		log.Debug().Msgf("[Init] running in debug mode")
		log.Debug().Msgf("[App] data path: %s", flag.Path)

		log.Debug().Msgf("[Git] git tag: %s", version.GitTag)
		log.Debug().Msgf("[Git] git commit: %s", version.GitCommit)
		log.Debug().Msgf("[Git] git short: %s", version.GitShort)
		log.Debug().Msgf("[Git] build date: %s", version.BuildDate)
		log.Debug().Msgf("[Runtime] version: %s %s", version.GoVersion, version.Platform)

		for _, mod := range version.Modules {
			log.Debug().Msgf("[Module] %s %s %s", mod.Path, mod.Version, mod.Sum)
		}
	}

	// CUPS passes exactly 5 or 6 positional arguments (plus argv[0]).
	if len(os.Args) < 6 {
		log.Info().Msg("Usage: metafilter job-id user title copies options [filename]")
		log.Error().Msgf("metafilter: expected 5 or 6 arguments, got %d", len(os.Args)-1)
		return
	}

	// argv[1] job-id, argv[2] user, argv[3] title, argv[4] copies,
	// argv[5] options, argv[6] (optional) filename.
	// user := os.Args[2]
	title := os.Args[3]

	// Open input: file argument takes precedence over stdin.
	input, closeInput, sourcePath, err := openInput(os.Args)
	if err != nil {
		log.Error().Err(err).Msg("metafilter: open input")
		return
	}
	defer closeInput()

	meta := buildMetadata(title, sourcePath)

	// Determine content type from the CUPS environment variable.
	contentType := os.Getenv("CONTENT_TYPE")
	if contentType == "" {
		contentType = "application/pdf" // safe default
	}

	if err := filter.Prepend(contentType, meta, input, os.Stdout); err != nil {
		log.Error().Err(err).Msg("metafilter: prepend metadata")
		return
	}
}

// openInput returns a reader for the print-job content.
// If argv[6] is provided and non-empty, the file is opened; otherwise stdin
// is returned. The returned closer must be called when done.
func openInput(args []string) (io.Reader, func(), string, error) {
	if len(args) >= 7 && args[6] != "" {
		f, err := os.Open(args[6]) // #nosec G304 G703 -- path is resolved from CUPS/db metadata in this flow
		if err != nil {
			return nil, func() {}, "", err
		}
		return f, func() {
			if err := f.Close(); err != nil {
				log.Error().Err(err).Str("path", args[6]).Msg("metafilter: close input file")
			}
		}, args[6], nil
	}
	return os.Stdin, func() {}, "", nil
}

// buildMetadata constructs the Metadata struct for a print job.
// It first tries to look up the original file path from the daemon's database;
// if that fails (db unavailable or no matching record) it falls back to
// using the job title as both filename and path.
func buildMetadata(title, sourcePath string) *filter.Metadata {
	meta := &filter.Metadata{
		Filename:  filepath.Base(title),
		Filepath:  title,
		PrintTime: time.Now(),
	}

	cfg := config.Get()

	store, err := database.New(filepath.Join(cfg.DatabaseDir, config.Get().DaemonDB.Name+".db"))
	if err != nil {
		// Database not available – use job-title metadata only.
		return meta
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Error().Err(err).Msg("metafilter: close database")
		}
	}()

	if sourcePath != "" {
		if deviceID, inodeNum, err := statIdentity(sourcePath); err != nil {
			log.Warn().Err(err).Str("path", sourcePath).Msg("metafilter: stat input")
		} else if record, err := store.LookupByDevInode(deviceID, inodeNum); err == nil {
			meta.Filepath = record.Filepath
			if err := store.MarkPrinted(record.ID); err != nil {
				log.Error().Err(err).Msg("metafilter: mark printed")
			}
			return meta
		} else if err != sql.ErrNoRows {
			log.Error().Err(err).Msg("metafilter: db inode lookup")
		}

		hash, err := hashFile(sourcePath)
		if err != nil {
			log.Warn().Err(err).Str("path", sourcePath).Msg("metafilter: hash input")
		} else if record, err := store.LookupByFileHash(hash); err == nil {
			meta.Filepath = record.Filepath
			if err := store.MarkPrinted(record.ID); err != nil {
				log.Error().Err(err).Msg("metafilter: mark printed")
			}
			return meta
		} else if err != sql.ErrNoRows {
			log.Error().Err(err).Msg("metafilter: db hash lookup")
		}
	}

	record, err := store.LookupByFilename(meta.Filename)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Error().Err(err).Msg("metafilter: db lookup")
		}
		return meta
	}

	if err := store.MarkPrinted(record.ID); err != nil {
		log.Error().Err(err).Msg("metafilter: mark printed")
	}
	// Enrich with the full path recorded by the daemon.
	meta.Filepath = record.Filepath
	return meta
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path) // #nosec G703 -- path
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Error().Err(err).Str("path", path).Msg("metafilter: close file")
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
