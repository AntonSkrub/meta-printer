//nolint:testpackage // Tests cover the daemon's internal hash cache.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHashCacheReusesAndInvalidatesHashes(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sample.pdf")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := newFileHashCache()
	calls := 0
	cache.compute = func(path string) (string, error) {
		calls++
		return hashFile(path)
	}

	first, err := cache.hash(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := cache.hash(path)
	if err != nil || again != first || calls != 1 {
		t.Fatalf("cached hash = %q, err = %v, hash calls = %d", again, err, calls)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := cache.hash(path)
	if err != nil || changed == first || calls != 2 {
		t.Fatalf("changed hash = %q, err = %v, hash calls = %d", changed, err, calls)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.hash(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: got %v", err)
	}
}

func TestHashCacheDoesNotLoopOnReadFailure(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sample.pdf")
	if err := os.WriteFile(path, []byte("document"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := newFileHashCache()
	failure := errors.New("read failed")
	calls := 0
	cache.compute = func(string) (string, error) {
		calls++
		return "", failure
	}
	for i := 0; i < 2; i++ {
		if _, err := cache.hash(path); !errors.Is(err, failure) {
			t.Fatalf("hash: got %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("hash attempts = %d; want 1", calls)
	}
	entry := cache.files[path]
	entry.retryAfter = time.Now().Add(-time.Second)
	cache.files[path] = entry
	if _, err := cache.hash(path); !errors.Is(err, failure) || calls != 2 {
		t.Fatalf("expected a retry after the delay: err = %v, calls = %d", err, calls)
	}
}
