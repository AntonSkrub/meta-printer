//nolint:testpackage // Tests intentionally use package internals in this file.
package database

import (
	"context"
	"database/sql"
	"errors"
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
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func TestRecordAndLookup(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	if err := s.RecordOpen("report.pdf", "/home/user/docs/report.pdf", "", "", 0, 0); err != nil {
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

func TestLookupByJobTitle(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	for i := 0; i < 2; i++ {
		if err := s.RecordOpen("three-page.pdf", "/docs/three-page.pdf", "", "Sample PDF", 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	record, err := s.LookupByJobTitle("Sample PDF")
	if err != nil || record.Filename != "three-page.pdf" {
		t.Fatalf("lookup = %+v, %v", record, err)
	}
	record, err = s.LookupByJobTitle("three-page.pdf")
	if err != nil || record.Filepath != "/docs/three-page.pdf" {
		t.Fatalf("filename lookup = %+v, %v", record, err)
	}
	record, err = s.LookupByJobTitle("/docs/three-page.pdf")
	if err != nil || record.Filepath != "/docs/three-page.pdf" {
		t.Fatalf("path-shaped job title lookup = %+v, %v", record, err)
	}
	if err := s.RecordOpen("other.pdf", "/docs/other.pdf", "", "Sample PDF", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LookupByJobTitle("Sample PDF"); !errors.Is(err, ErrAmbiguousJobTitle) {
		t.Fatalf("duplicate title: %v", err)
	}
	if err := s.RecordOpen("other.pdf", "/docs/other.pdf", "", "New title", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LookupByJobTitle("Sample PDF"); err != nil {
		t.Fatalf("old title should not make a current match ambiguous: %v", err)
	}
	if err := s.RecordOpen("Sample PDF", "/docs/Sample PDF", "", "", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LookupByJobTitle("Sample PDF"); !errors.Is(err, ErrAmbiguousJobTitle) {
		t.Fatalf("title/filename collision: %v", err)
	}
	if _, err := s.LookupByJobTitle("missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing title: %v", err)
	}
}

func TestMigrationPreservesExistingRecords(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE file_metadata (
		id INTEGER PRIMARY KEY AUTOINCREMENT, filename TEXT NOT NULL, filepath TEXT NOT NULL,
		file_hash TEXT, dev_id TEXT, inode_num TEXT, opened_at DATETIME NOT NULL, printed_at DATETIME
	); INSERT INTO file_metadata (filename, filepath, opened_at) VALUES ('old.pdf', '/docs/old.pdf', CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.LookupByJobTitle("old.pdf"); err != nil {
		t.Fatalf("old record: %v", err)
	}
	if err := s.RecordOpen("new.pdf", "/docs/new.pdf", "", "New title", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LookupByJobTitle("New title"); err != nil {
		t.Fatalf("new title: %v", err)
	}
}

func TestLookupReturnsLatest(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	if err := s.RecordOpen("doc.txt", "/old/doc.txt", "", "", 0, 0); err != nil {
		t.Fatal(err)
	}
	// Ensure the second record has a strictly later timestamp.
	time.Sleep(2 * time.Millisecond)
	if err := s.RecordOpen("doc.txt", "/new/doc.txt", "", "", 0, 0); err != nil {
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
	t.Parallel()

	s := newTestStore(t)

	if err := s.RecordOpen("file.docx", "/tmp/file.docx", "", "", 0, 0); err != nil {
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
	t.Parallel()

	s := newTestStore(t)
	_, err := s.LookupByFilename("nonexistent.pdf")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDeleteOpenedBefore(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	cutoff := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	records := []struct {
		name     string
		openedAt any
		printed  any
		expired  bool
	}{
		{"old.pdf", cutoff.Add(-time.Hour), cutoff, true},
		{"near-old.pdf", cutoff.Add(-time.Nanosecond), nil, true},
		{"legacy.pdf", "2026-11-24 09:59:59", nil, true},
		{"boundary.pdf", cutoff, nil, false},
		{"new.pdf", cutoff.Add(time.Nanosecond), nil, false},
	}
	for _, record := range records {
		_, err := s.db.Exec(`INSERT INTO file_metadata (filename, filepath, opened_at, printed_at) VALUES (?, ?, ?, ?)`,
			record.name, "/docs/"+record.name, record.openedAt, record.printed)
		if err != nil {
			t.Fatal(err)
		}
	}
	count, err := s.DeleteOpenedBefore(cutoff.In(time.FixedZone("local", 3600)))
	if err != nil || count != 3 {
		t.Fatalf("deleted %d records, error %v; want 3", count, err)
	}
	for _, record := range records {
		_, err := s.LookupByFilename(record.name)
		if record.expired && !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("%s: expected deletion, got %v", record.name, err)
		}
		if !record.expired && err != nil {
			t.Errorf("%s: expected retained record, got %v", record.name, err)
		}
	}
	count, err = s.DeleteOpenedBefore(cutoff)
	if err != nil || count != 0 {
		t.Fatalf("repeat cleanup deleted %d records, error %v", count, err)
	}
}

func TestDeleteOpenedBeforeReportsDatabaseErrors(t *testing.T) {
	t.Parallel()
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteOpenedBefore(time.Now()); err == nil {
		t.Fatal("expected cleanup error on closed database")
	}
}

func TestDeleteOpenedBeforeClearsExpiredTitleAmbiguity(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	cutoff := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	for _, record := range []struct {
		path string
		time time.Time
	}{
		{"/old/sample.pdf", cutoff.Add(-time.Hour)},
		{"/new/sample.pdf", cutoff.Add(time.Hour)},
	} {
		_, err := s.db.Exec(`INSERT INTO file_metadata (filename, filepath, title, opened_at) VALUES (?, ?, ?, ?)`,
			"sample.pdf", record.path, "Sample PDF", record.time)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.LookupByJobTitle("Sample PDF"); !errors.Is(err, ErrAmbiguousJobTitle) {
		t.Fatalf("expected ambiguity before cleanup, got %v", err)
	}
	if _, err := s.DeleteOpenedBefore(cutoff); err != nil {
		t.Fatal(err)
	}
	record, err := s.LookupByJobTitle("Sample PDF")
	if err != nil || record.Filepath != "/new/sample.pdf" {
		t.Fatalf("lookup after cleanup = %+v, %v", record, err)
	}
}

func TestLookupByFileHashDistinguishesSameFilename(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	if err := s.RecordOpen("invoice.pdf", "/home/user/desktop/invoice.pdf", "hash-desktop", "", 2050, 382174); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordOpen("invoice.pdf", "/home/user/documents/invoice.pdf", "hash-documents", "", 2050, 445192); err != nil {
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

func TestLookupByFileHashRejectsDistinctPaths(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	for i := 0; i < 2; i++ {
		if err := s.RecordOpen("one.pdf", "/docs/one.pdf", "same", "", 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	record, err := s.LookupByFileHash("same")
	if err != nil || record.Filepath != "/docs/one.pdf" {
		t.Fatalf("repeated opens of one path: %+v, %v", record, err)
	}
	if err := s.RecordOpen("two.pdf", "/downloads/two.pdf", "same", "", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LookupByFileHash("same"); !errors.Is(err, ErrAmbiguousFileHash) {
		t.Fatalf("expected ambiguity for identical files: %v", err)
	}
	if err := s.RecordOpen("two.pdf", "/downloads/two.pdf", "changed", "", 0, 0); err != nil {
		t.Fatal(err)
	}
	record, err = s.LookupByFileHash("same")
	if err != nil || record.Filepath != "/docs/one.pdf" {
		t.Fatalf("latest record should replace old hash: %+v, %v", record, err)
	}
	if err := s.RecordOpen("one.pdf", "/docs/one.pdf", "", "", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LookupByFileHash("same"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("latest missing hash should not reuse old hash: %v", err)
	}
	if _, err := s.LookupByFileHash(""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("empty hash must not match: %v", err)
	}
}

func TestLookupByDevInode(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	if err := s.RecordOpen("invoice.pdf", "/home/user/desktop/invoice.pdf", "hash-open", "", 2050, 382174); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordOpen("invoice.pdf", "/home/user/documents/invoice.pdf", "hash-edited", "", 2050, 382174); err != nil {
		t.Fatal(err)
	}

	m, err := s.LookupByDevInode(2050, 382174)
	if err != nil {
		t.Fatalf("LookupByDevInode: %v", err)
	}

	if m.Filepath != "/home/user/documents/invoice.pdf" {
		t.Errorf("expected latest inode/dev match, got %q", m.Filepath)
	}
	if m.DeviceID != 2050 {
		t.Errorf("expected device id 2050, got %d", m.DeviceID)
	}
	if m.InodeNum != 382174 {
		t.Errorf("expected inode 382174, got %d", m.InodeNum)
	}
}

func TestNew_CreatesDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "nested", "subdir")
	path := filepath.Join(dir, "meta.db")

	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = s.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("database file not created: %v", err)
	}
}

func TestMarkPrintedWaitsForConcurrentWriter(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "shared.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	other, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	if err := s.RecordOpen("report.pdf", "/home/user/report.pdf", "", "", 0, 0); err != nil {
		t.Fatal(err)
	}
	record, err := s.LookupByFilename("report.pdf")
	if err != nil {
		t.Fatal(err)
	}

	conn, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")

	done := make(chan error, 1)
	go func() { done <- other.MarkPrinted(record.ID) }()
	select {
	case err := <-done:
		t.Fatalf("write returned before lock was released: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if _, err := conn.ExecContext(context.Background(), "COMMIT"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write after lock released: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("write did not finish after lock was released")
	}
}

func TestDatabasePathWithSpecialCharacters(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata #1.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not created at requested path: %v", err)
	}
}
