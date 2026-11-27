package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/AntonSkrub/meta-printer/pkg/database"
	"github.com/AntonSkrub/meta-printer/pkg/filter"
)

func TestLookupMetadataByPDFTitle(t *testing.T) {
	store, err := database.New(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.RecordOpen("three-page.pdf", "/docs/three-page.pdf", "", "Sample PDF", 0, 0); err != nil {
		t.Fatal(err)
	}
	meta := &filter.Metadata{Filename: "Sample PDF", Filepath: "unavailable - lookup error"}
	result := lookupMetadata(store, "Sample PDF", bytes.NewReader(nil), "")
	got := metadataForMatch(store, meta, "Sample PDF", result)
	if got.Filename != "three-page.pdf" || got.Filepath != "/docs/three-page.pdf" {
		t.Fatalf("matched metadata: %+v", got)
	}
	record, err := store.LookupByJobTitle("Sample PDF")
	if err != nil || record.PrintedAt == nil {
		t.Fatalf("record was not marked printed: %+v, %v", record, err)
	}
	if err := store.RecordOpen("other.pdf", "/docs/other.pdf", "", "Sample PDF", 0, 0); err != nil {
		t.Fatal(err)
	}
	meta = &filter.Metadata{Filename: "Sample PDF", Filepath: "unavailable - lookup error"}
	result = lookupMetadata(store, "Sample PDF", bytes.NewReader(nil), "")
	got = metadataForMatch(store, meta, "Sample PDF", result)
	if got.Filename != "Sample PDF" || got.Filepath != "unavailable - ambiguous match" {
		t.Fatalf("ambiguous title should retain fallback: %+v", got)
	}

	input := []byte("original content")
	path := filepath.Join(t.TempDir(), "spool")
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordOpen("three-page.pdf", "/docs/three-page.pdf", hashBytes(input), "Sample PDF", 0, 0); err != nil {
		t.Fatal(err)
	}
	meta = &filter.Metadata{Filename: "Sample PDF", Filepath: "unavailable - lookup error"}
	result = lookupMetadata(store, "Sample PDF", bytes.NewReader(input), path)
	got = metadataForMatch(store, meta, "Sample PDF", result)
	if got.Filename != "three-page.pdf" || got.Filepath != "/docs/three-page.pdf" {
		t.Fatalf("strong hash match should take precedence: %+v", got)
	}
}

func TestLookupMetadataAmbiguousHashAndFailures(t *testing.T) {
	store, err := database.New(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	input := []byte("identical")
	path := filepath.Join(t.TempDir(), "spool")
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct{ name, path, title string }{
		{"one.pdf", "/docs/one.pdf", "First"},
		{"two.pdf", "/docs/two.pdf", "Second"},
		{"two.pdf", "/docs/two.pdf", "Second"},
	} {
		if err := store.RecordOpen(file.name, file.path, hashBytes(input), file.title, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	result := lookupMetadata(store, "First", bytes.NewReader(input), path)
	if result.status != matchAmbiguous {
		t.Fatalf("duplicate hash must remain ambiguous even when title differs: %+v", result)
	}
	meta := metadataForMatch(store, &filter.Metadata{Filename: "First", Filepath: "unavailable - lookup error"}, "First", result)
	if meta.Filepath != "unavailable - ambiguous match" {
		t.Fatalf("unexpected ambiguous path: %+v", meta)
	}
	for _, title := range []string{"First", "Second"} {
		record, err := store.LookupByJobTitle(title)
		if err != nil || record.PrintedAt != nil {
			t.Fatalf("ambiguous match marked a record printed: %+v, %v", record, err)
		}
	}
	result = lookupMetadata(store, "Unknown", bytes.NewReader(nil), "")
	meta = metadataForMatch(store, &filter.Metadata{Filename: "Unknown", Filepath: "unavailable - lookup error"}, "Unknown", result)
	if result.status != matchNotFound || meta.Filepath != "unavailable - no match" {
		t.Fatalf("missing match: %+v %+v", result, meta)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	result = lookupMetadata(store, "Unknown", bytes.NewReader(nil), "")
	meta = metadataForMatch(store, &filter.Metadata{Filename: "Unknown", Filepath: "unavailable - lookup error"}, "Unknown", result)
	if result.status != matchError || result.err == nil || meta.Filepath != "unavailable - lookup error" {
		t.Fatalf("lookup error: %+v %+v", result, meta)
	}
}
