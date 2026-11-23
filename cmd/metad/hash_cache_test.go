//nolint:testpackage // Tests cover the daemon's internal hash cache.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testPDF(title string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	objects := []string{
		"<</Type /Catalog /Pages 2 0 R>>",
		"<</Type /Pages /Kids [3 0 R] /Count 1>>",
		"<</Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources <<>> /Contents 4 0 R>>",
		"<</Length 0>>\nstream\n\nendstream",
		fmt.Sprintf("<</Title (%s)>>", title),
	}
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<</Size %d /Root 1 0 R /Info 5 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func TestPDFTitleCache(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sample.pdf")
	if err := os.WriteFile(path, testPDF("Sample PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := newFileHashCache()
	calls := 0
	cache.readTitle = func(path string) (string, error) {
		calls++
		return readPDFTitle(path)
	}
	for i := 0; i < 2; i++ {
		title, err := cache.title(path)
		if err != nil || title != "Sample PDF" {
			t.Fatalf("title = %q, err = %v", title, err)
		}
	}
	if calls != 1 {
		t.Fatalf("title reads = %d, want 1", calls)
	}
	if err := os.WriteFile(path, testPDF("Other title"), 0o600); err != nil {
		t.Fatal(err)
	}
	if title, err := cache.title(path); err != nil || title != "Other title" || calls != 2 {
		t.Fatalf("changed title = %q, err = %v, reads = %d", title, err, calls)
	}
	if err := os.WriteFile(path, testPDF(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if title, err := cache.title(path); err != nil || title != "" || calls != 3 {
		t.Fatalf("untitled PDF = %q, err = %v, reads = %d", title, err, calls)
	}
	if err := os.WriteFile(path, []byte("not a PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := cache.title(path); err == nil {
			t.Fatal("invalid PDF should produce an error")
		}
	}
	if calls != 4 {
		t.Fatalf("invalid PDF was repeatedly reopened: %d reads", calls)
	}
}

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
