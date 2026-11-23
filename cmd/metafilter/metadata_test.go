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
	meta := &filter.Metadata{Filename: "Sample PDF", Filepath: "Sample PDF"}
	got := lookupMetadata(store, meta, "Sample PDF", bytes.NewReader(nil), "")
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
	meta = &filter.Metadata{Filename: "Sample PDF", Filepath: "Sample PDF"}
	got = lookupMetadata(store, meta, "Sample PDF", bytes.NewReader(nil), "")
	if got.Filename != "Sample PDF" || got.Filepath != "Sample PDF" {
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
	meta = &filter.Metadata{Filename: "Sample PDF", Filepath: "Sample PDF"}
	got = lookupMetadata(store, meta, "Sample PDF", bytes.NewReader(input), path)
	if got.Filename != "three-page.pdf" || got.Filepath != "/docs/three-page.pdf" {
		t.Fatalf("strong hash match should take precedence: %+v", got)
	}
}
