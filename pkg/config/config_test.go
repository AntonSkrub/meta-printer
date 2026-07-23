package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefault_HasDaemonDB(t *testing.T) {
	cfg := Default()
	if cfg.DaemonDB == "" {
		t.Error("DaemonDB should not be empty")
	}
}

func TestDefault_FilterDBDir(t *testing.T) {
	cfg := Default()
	if cfg.FilterDBDir != "/var/lib/meta-printer" {
		t.Errorf("FilterDBDir: got %q, want %q", cfg.FilterDBDir, "/var/lib/meta-printer")
	}
}

func TestLoad_NoConfigFile(t *testing.T) {
	// Point XDG_CONFIG_HOME to an empty temp dir so no config file is found.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	def := Default()
	if cfg.DaemonDB != def.DaemonDB {
		t.Errorf("DaemonDB: got %q, want %q", cfg.DaemonDB, def.DaemonDB)
	}
	if cfg.FilterDBDir != def.FilterDBDir {
		t.Errorf("FilterDBDir: got %q, want %q", cfg.FilterDBDir, def.FilterDBDir)
	}
}

func TestLoad_OverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgDir := filepath.Join(dir, "meta-printer")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	want := Config{
		DaemonDB:    "/tmp/test.db",
		WatchDirs:   []string{"/tmp/watch1", "/tmp/watch2"},
		FilterDBDir: "/tmp/filter",
	}
	data, _ := json.Marshal(want)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.DaemonDB != want.DaemonDB {
		t.Errorf("DaemonDB: got %q, want %q", got.DaemonDB, want.DaemonDB)
	}
	if got.FilterDBDir != want.FilterDBDir {
		t.Errorf("FilterDBDir: got %q, want %q", got.FilterDBDir, want.FilterDBDir)
	}
	if len(got.WatchDirs) != len(want.WatchDirs) {
		t.Errorf("WatchDirs length: got %d, want %d", len(got.WatchDirs), len(want.WatchDirs))
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgDir := filepath.Join(dir, "meta-printer")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil {
		t.Error("Load should return an error for invalid JSON")
	}
}
