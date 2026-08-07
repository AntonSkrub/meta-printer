//nolint:testpackage // Tests intentionally cover unexported config helpers.
package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultWatchDirs_FindsExistingDirs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	for _, dir := range []string{"Documents", "Downloads", "Desktop"} {
		if err := os.Mkdir(filepath.Join(home, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}

	got := defaultWatchDirs(home)
	want := []string{
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Downloads"),
		filepath.Join(home, "Desktop"),
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("defaultWatchDirs: got %v, want %v", got, want)
	}
}

func TestDefaultWatchDirs_EmptyHome(t *testing.T) {
	t.Parallel()

	if got := defaultWatchDirs(""); got != nil {
		t.Errorf("defaultWatchDirs: got %v, want nil", got)
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	cfg := Config{
		LogLevel:    -1,
		WatchDirs:   []string{"/tmp/watch"},
		DatabaseDir: "/var/lib/meta-printer",
		DaemonDB: DatabaseConfig{
			Driver: "sqlite",
			Name:   "metadata",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestConfigValidate_RejectsEmptyWatchDirs(t *testing.T) {
	t.Parallel()

	cfg := Config{
		LogLevel:    -1,
		DatabaseDir: "/var/lib/meta-printer",
		DaemonDB: DatabaseConfig{
			Driver: "sqlite",
			Name:   "metadata",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate should reject empty watch dirs")
	}
}

func TestConfigValidate_RejectsMissingDatabaseDir(t *testing.T) {
	t.Parallel()

	cfg := Config{
		LogLevel:  -1,
		WatchDirs: []string{"/tmp/watch"},
		DaemonDB: DatabaseConfig{
			Driver: "sqlite",
			Name:   "metadata",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate should reject empty database dir")
	}
}

func TestDatabaseConfigValidate_SQLiteRequiresName(t *testing.T) {
	t.Parallel()

	dc := DatabaseConfig{Driver: "sqlite"}
	if err := dc.Validate(); err == nil {
		t.Fatal("Validate should reject sqlite config without a name")
	}
}

func TestDatabaseConfigValidate_NonSQLiteRequiresConnectionFields(t *testing.T) {
	t.Parallel()

	dc := DatabaseConfig{Driver: "postgres"}
	if err := dc.Validate(); err == nil {
		t.Fatal("Validate should reject incomplete non-sqlite config")
	}

	dc = DatabaseConfig{
		Driver:   "postgres",
		Host:     "localhost",
		Port:     5432,
		User:     "user",
		Password: "secret",
		Name:     "meta",
	}
	if err := dc.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
