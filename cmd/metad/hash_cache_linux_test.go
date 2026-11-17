//go:build linux

//nolint:testpackage // Test exercises the daemon's actual inotify feedback.
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AntonSkrub/meta-printer/pkg/watcher"
)

func TestHashingWatchedFileStopsGeneratingEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.pdf")
	if err := os.WriteFile(path, []byte("document"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := watcher.New()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	if err := w.Add(dir); err != nil {
		t.Fatal(err)
	}
	w.Start()
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}

	cache := newFileHashCache()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	count := 0
	for {
		select {
		case ev := <-w.Events:
			count++
			if _, err := cache.hash(ev.Path); err != nil {
				t.Fatal(err)
			}
			if count > 2 {
				t.Fatal("hashing keeps generating open events")
			}
		case err := <-w.Errors:
			t.Fatal(err)
		case <-timeout.C:
			if count != 2 {
				t.Fatalf("received %d events, want external open and one hash open", count)
			}
			return
		}
	}
}
