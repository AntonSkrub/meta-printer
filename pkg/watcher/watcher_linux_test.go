//go:build linux

//nolint:testpackage // Tests inspect watch registration to avoid timing races.
package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitForWatch(t *testing.T, w *Watcher, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		found := false
		for _, watched := range w.wds {
			if watched == path {
				found = true
				break
			}
		}
		w.mu.Unlock()
		if found {
			return
		}
		select {
		case err := <-w.Errors:
			t.Fatalf("watcher error: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("watch for %s not installed", path)
}

func expectOpen(t *testing.T, w *Watcher, path string) {
	t.Helper()
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-w.Events:
		if ev.Path != path {
			t.Fatalf("event path = %q, want %q", ev.Path, path)
		}
	case err := <-w.Errors:
		t.Fatalf("watcher error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatalf("no open event for %s", path)
	}
}

func TestRecursiveWatching(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing", "nested")
	if err := os.MkdirAll(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	existingFile := filepath.Join(existing, "existing.pdf")
	if err := os.WriteFile(existingFile, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	preparedFile := filepath.Join(outside, "new.pdf")
	if err := os.WriteFile(preparedFile, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	if err := w.Add(root); err != nil {
		t.Fatal(err)
	}
	w.Start()
	expectOpen(t, w, existingFile)

	created := filepath.Join(root, "created")
	if err := os.Mkdir(created, 0o700); err != nil {
		t.Fatal(err)
	}
	waitForWatch(t, w, created)
	nested := filepath.Join(created, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	waitForWatch(t, w, nested)
	newFile := filepath.Join(nested, "new.pdf")
	if err := os.Rename(preparedFile, newFile); err != nil {
		t.Fatal(err)
	}
	expectOpen(t, w, newFile)

	renamed := filepath.Join(root, "renamed")
	if err := os.Rename(created, renamed); err != nil {
		t.Fatal(err)
	}
	renamedNested := filepath.Join(renamed, "nested")
	waitForWatch(t, w, renamedNested)
	expectOpen(t, w, filepath.Join(renamedNested, "new.pdf"))

	moved := filepath.Join(outside, "moved")
	if err := os.Rename(renamed, moved); err != nil {
		t.Fatal(err)
	}
	// A later event in the same parent confirms the move has been processed.
	barrier := filepath.Join(root, "barrier")
	if err := os.Mkdir(barrier, 0o700); err != nil {
		t.Fatal(err)
	}
	waitForWatch(t, w, barrier)
	if _, err := os.ReadFile(filepath.Join(moved, "nested", "new.pdf")); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-w.Events:
		t.Fatalf("unexpected event outside watched root: %+v", ev)
	case err := <-w.Errors:
		t.Fatalf("watcher error: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	incoming := filepath.Join(root, "incoming")
	if err := os.Rename(moved, incoming); err != nil {
		t.Fatal(err)
	}
	waitForWatch(t, w, filepath.Join(incoming, "nested"))
	expectOpen(t, w, filepath.Join(incoming, "nested", "new.pdf"))
}

func TestRecursiveWatchingDoesNotFollowSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.pdf"), []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	w, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	if err := w.Add(root); err != nil {
		t.Fatal(err)
	}
	w.Start()
	if _, err := os.ReadFile(filepath.Join(outside, "outside.pdf")); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-w.Events:
		t.Fatalf("symlink target was watched: %+v", ev)
	case err := <-w.Errors:
		t.Fatal(err)
	case <-time.After(200 * time.Millisecond):
	}
}
