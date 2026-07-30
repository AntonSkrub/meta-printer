package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRecordAndLookup(t *testing.T) {
	s := newTestStore(t)

	if err := s.RecordOpen("report.pdf", "/home/user/docs/report.pdf", ""); err != nil {
		t.Fatalf("RecordOpen: %v", err)
	}

	m, err := s.LookupByFilename("report.pdf")
	if err != nil {
		t.Fatalf("LookupByFilename: %v", err)
	}

	if m.Filename != "report.pdf" {
		t.Errorf("Filename: got %q, want %q", m.Filename, "report.pdf")
	}
	if m.Filepath != "/home/user/docs/report.pdf" {
		t.Errorf("Filepath: got %q, want %q", m.Filepath, "/home/user/docs/report.pdf")
	}
	if m.PrintedAt != nil {
		t.Error("PrintedAt should be nil before MarkPrinted")
	}
}

func TestLookupReturnsLatest(t *testing.T) {
	s := newTestStore(t)

	if err := s.RecordOpen("doc.txt", "/old/doc.txt", ""); err != nil {
		t.Fatal(err)
	}
	// Ensure the second record has a strictly later timestamp.
	time.Sleep(2 * time.Millisecond)
	if err := s.RecordOpen("doc.txt", "/new/doc.txt", ""); err != nil {
		t.Fatal(err)
	}

	m, err := s.LookupByFilename("doc.txt")
	if err != nil {
		t.Fatalf("LookupByFilename: %v", err)
	}
	if m.Filepath != "/new/doc.txt" {
		t.Errorf("expected /new/doc.txt, got %q", m.Filepath)
	}
}

func TestMarkPrinted(t *testing.T) {
	s := newTestStore(t)

	if err := s.RecordOpen("file.docx", "/tmp/file.docx", ""); err != nil {
		t.Fatal(err)
	}
	m, err := s.LookupByFilename("file.docx")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.MarkPrinted(m.ID); err != nil {
		t.Fatalf("MarkPrinted: %v", err)
	}

	m2, err := s.LookupByFilename("file.docx")
	if err != nil {
		t.Fatal(err)
	}
	if m2.PrintedAt == nil {
		t.Error("PrintedAt should be set after MarkPrinted")
	}
}

func TestLookupMissing(t *testing.T) {
	s := newTestStore(t)
	_, err := s.LookupByFilename("nonexistent.pdf")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestLookupByFileHashDistinguishesSameFilename(t *testing.T) {
	s := newTestStore(t)

	if err := s.RecordOpen("invoice.pdf", "/home/user/desktop/invoice.pdf", "hash-desktop"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordOpen("invoice.pdf", "/home/user/documents/invoice.pdf", "hash-documents"); err != nil {
		t.Fatal(err)
	}

	m, err := s.LookupByFileHash("hash-desktop")
	if err != nil {
		t.Fatalf("LookupByFileHash: %v", err)
	}
	if m.Filepath != "/home/user/desktop/invoice.pdf" {
		t.Errorf("expected desktop path, got %q", m.Filepath)
	}

	m, err = s.LookupByFileHash("hash-documents")
	if err != nil {
		t.Fatalf("LookupByFileHash: %v", err)
	}
	if m.Filepath != "/home/user/documents/invoice.pdf" {
		t.Errorf("expected documents path, got %q", m.Filepath)
	}

	m, err = s.LookupByFilename("invoice.pdf")
	if err != nil {
		t.Fatalf("LookupByFilename: %v", err)
	}
	if m.Filepath != "/home/user/documents/invoice.pdf" {
		t.Errorf("expected latest filename match, got %q", m.Filepath)
	}
}

func TestNew_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "subdir")
	path := filepath.Join(dir, "meta.db")

	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.Close()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("database file not created: %v", err)
	}
}
