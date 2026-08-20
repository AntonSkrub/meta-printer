// metad is the Meta-Printer metadata daemon.
//
// It watches configured directories using Linux inotify and records metadata
// (filename, full path, open timestamp) to a SQLite database whenever a
// supported document file is opened.  The CUPS metafilter reads this database
// to look up file metadata at print time.
//
// Usage:
//
//	metad [flags]
//
// Flags:
//
//	--path         application data directory (default: ~/.local/share/metad)
//	--watch-dirs   list of directories to watch
//	--log-level    log verbosity (-1 = trace … 5 = panic)
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/AntonSkrub/meta-printer/pkg/config"
	"github.com/AntonSkrub/meta-printer/pkg/database"
	"github.com/AntonSkrub/meta-printer/pkg/watcher"
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

	zerolog.SetGlobalLevel(zerolog.Level(config.Get().LogLevel))
}

func main() {
	defer interruption.Catch()

	if flag.Help {
		flag.PrintHelp()
		return
	}

	if flag.Version {
		log.Info().Str("version", version.String()).Msg("metad")
		fmt.Print(version.String())
		return
	}

	log.Info().Msgf("=== Meta-Printer daemon %s ===", version.String())
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

	cfg := config.Get()

	dbPath := filepath.Join(cfg.DatabaseDir, cfg.DaemonDB.Name+".db")
	store, err := database.New(dbPath)
	if err != nil {
		log.Error().Err(err).Msg("metad: open database")
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Error().Err(err).Msg("metad: close database")
		}
	}()

	w, err := watcher.New()
	if err != nil {
		log.Error().Err(err).Msg("metad: create watcher")
	}
	defer w.Stop()

	for _, dir := range cfg.WatchDirs {
		if err := w.Add(dir); err != nil {
			log.Warn().Err(err).Str("dir", dir).Msg("metad: cannot watch directory")
		} else {
			log.Debug().Str("dir", dir).Msg("metad: watching")
		}
	}

	w.Start()
	log.Info().Str("database", dbPath).Msg("metad: started")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	for {
		select {
		case ev := <-w.Events:
			hash, err := hashFile(ev.Path)
			if err != nil {
				log.Warn().Err(err).Str("path", ev.Path).Msg("metad: hash file")
			}

			if err := store.RecordOpen(ev.Name, ev.Path, hash, ev.DeviceID, ev.InodeNum); err != nil {
				log.Error().Err(err).Str("path", ev.Path).Msg("metad: record open")
			} else {
				log.Debug().Str("path", ev.Path).Msg("metad: recorded")
			}
		case err := <-w.Errors:
			log.Error().Err(err).Msg("metad: watcher error")
		case <-sig:
			log.Info().Msg("metad: shutting down")
			return
		}
	}
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 - path
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Error().Err(err).Str("path", path).Msg("metad: close file")
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
