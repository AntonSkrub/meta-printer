// Package config loads Meta-Printer runtime configuration.
//
// Configuration is read from the first JSON file found in the following
// locations (in order):
//
//  1. $XDG_CONFIG_HOME/meta-printer/config.json  (defaults to ~/.config/…)
//  2. /etc/meta-printer/config.json
//
// Missing or unreadable files are silently skipped.  Fields absent from the
// file retain their computed default values.
//
// Example config.json:
//
//	{
//	  "daemon_db":     "/home/alice/.local/share/meta-printer/metadata.db",
//	  "watch_dirs":    ["/home/alice/Documents", "/home/alice/Downloads"],
//	  "filter_db_dir": "/var/lib/meta-printer"
//	}
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds all configurable values for Meta-Printer.
type Config struct {
	// DaemonDB is the path to the SQLite database written by metad.
	DaemonDB string `json:"daemon_db"`

	// WatchDirs is the list of directories monitored by metad for file-open
	// events.
	WatchDirs []string `json:"watch_dirs"`

	// FilterDBDir is the directory that contains the per-user SQLite databases
	// read by metafilter at print time.
	FilterDBDir string `json:"filter_db_dir"`
}

// Default returns a Config populated with reasonable defaults derived from the
// current user's environment.  The watch-dirs list contains only the
// candidate directories that actually exist on the filesystem.
func Default() Config {
	home, _ := os.UserHomeDir()

	daemonDB := "/var/lib/meta-printer/metadata.db"
	if home != "" {
		daemonDB = filepath.Join(home, ".local", "share", "meta-printer", "metadata.db")
	}

	return Config{
		DaemonDB:    daemonDB,
		WatchDirs:   defaultWatchDirs(home),
		FilterDBDir: "/var/lib/meta-printer",
	}
}

// Load returns a Config by merging file-based settings over the defaults
// returned by Default.  The first config file found in the standard search
// paths is used; remaining paths are ignored.
func Load() (Config, error) {
	cfg := Default()

	for _, path := range searchPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue // file absent or unreadable – try next location
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("config: parse %s: %w", path, err)
		}
		break
	}

	return cfg, nil
}

// searchPaths returns the ordered list of config file locations to try.
func searchPaths() []string {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		home, _ := os.UserHomeDir()
		xdg = filepath.Join(home, ".config")
	}
	return []string{
		filepath.Join(xdg, "meta-printer", "config.json"),
		"/etc/meta-printer/config.json",
	}
}

// defaultWatchDirs returns the candidate watch directories that exist under
// the given home directory.
func defaultWatchDirs(home string) []string {
	if home == "" {
		return nil
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
	return existing
}
