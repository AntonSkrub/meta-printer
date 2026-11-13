//go:build linux

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOpenTrustedSpoolFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d00030-001")
	if err := os.WriteFile(path, []byte("job data"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openTrustedSpoolFile(dir, filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if closeErr := f.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil || string(data) != "job data" {
		t.Fatalf("read spool file = %q, %v", data, err)
	}

	if err := os.Symlink(path, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(dir, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"link", "fifo", "missing"} {
		t.Run(name, func(t *testing.T) {
			f, err := openTrustedSpoolFile(dir, name)
			if err == nil {
				f.Close()
				t.Fatalf("unexpectedly opened %s", name)
			}
		})
	}
	dirLink := filepath.Join(t.TempDir(), "spool-link")
	if err := os.Symlink(dir, dirLink); err != nil {
		t.Fatal(err)
	}
	if f, err := openTrustedSpoolFile(dirLink, "d00030-001"); err == nil {
		f.Close()
		t.Fatal("unexpectedly opened a symlinked spool directory")
	}
}

func TestOpenTrustedSpoolFileWithoutDirectoryReadPermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "job"), []byte("job data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o100); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, err := os.ReadDir(dir); err == nil {
		t.Fatal("expected directory listing to be denied")
	}
	f, err := openTrustedSpoolFile(dir, "job")
	if err != nil {
		t.Fatalf("open known file without directory read permission: %v", err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil || strings.TrimSpace(string(data)) != "job data" {
		t.Fatalf("read spool file = %q, %v", data, err)
	}
}
