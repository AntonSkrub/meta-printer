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
//	-db    path to SQLite metadata database (default: ~/.local/share/meta-printer/metadata.db)
//	-watch colon-separated list of directories to watch
//	       (default: ~/Documents:~/Downloads:~/Desktop)
//	-v     verbose output
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/AntonSkrub/meta-printer/pkg/db"
	"github.com/AntonSkrub/meta-printer/pkg/watcher"
)

func main() {
	dbPath := flag.String("db", defaultDBPath(), "path to SQLite metadata database")
	watchDirs := flag.String("watch", defaultWatchDirs(), "colon-separated directories to watch")
	verbose := flag.Bool("v", false, "verbose output")
	flag.Parse()

	store, err := db.New(*dbPath)
	if err != nil {
		log.Fatalf("metad: open database: %v", err)
	}
	defer store.Close()

	w, err := watcher.New()
	if err != nil {
		log.Fatalf("metad: create watcher: %v", err)
	}
	defer w.Stop()

	for _, dir := range splitDirs(*watchDirs) {
		if err := w.Add(dir); err != nil {
			log.Printf("metad: warning: cannot watch %q: %v", dir, err)
		} else if *verbose {
			log.Printf("metad: watching %q", dir)
		}
	}

	w.Start()

	log.Printf("metad: started – database: %s", *dbPath)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	for {
		select {
		case ev := <-w.Events:
			if err := store.RecordOpen(ev.Name, ev.Path); err != nil {
				log.Printf("metad: record open %q: %v", ev.Path, err)
			} else if *verbose {
				log.Printf("metad: recorded %q", ev.Path)
			}
		case err := <-w.Errors:
			log.Printf("metad: watcher error: %v", err)
		case <-sig:
			log.Println("metad: shutting down")
			return
		}
	}
}

// splitDirs returns the non-empty directories from a colon-separated list.
func splitDirs(s string) []string {
	var dirs []string
	for _, d := range strings.Split(s, ":") {
		d = strings.TrimSpace(d)
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// defaultDBPath returns the user-specific default database path.
func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/var/lib/meta-printer/metadata.db"
	}
	return filepath.Join(home, ".local", "share", "meta-printer", "metadata.db")
}

// defaultWatchDirs returns a colon-separated list of the standard user
// document directories that exist on the current system.
func defaultWatchDirs() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidates := []string{
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Downloads"),
		filepath.Join(home, "Desktop"),
	}
	var existing []string
	for _, d := range candidates {
		if _, err := os.Stat(d); err == nil {
			existing = append(existing, d)
		}
	}
	return strings.Join(existing, ":")
}
