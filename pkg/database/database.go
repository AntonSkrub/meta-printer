// Package db provides a SQLite-backed store for file-open metadata collected
// by the metadata daemon and consumed by the CUPS print filter.
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// FileMetadata holds the information recorded when a file is opened.
type FileMetadata struct {
	ID        int64
	Filename  string
	Filepath  string
	OpenedAt  time.Time
	PrintedAt *time.Time
}

// Store is a thread-safe metadata store backed by SQLite.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database at path, creating any parent
// directories as needed, and runs the schema migration.
func New(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("db: create parent directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}

	// Single-writer SQLite; enable WAL for better concurrency.
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("db: enable WAL: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("db: migrate: %w", err)
	}
	return s, nil
}

// migrate creates the schema if it does not already exist.
func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS file_metadata (
			id          INTEGER  PRIMARY KEY AUTOINCREMENT,
			filename    TEXT     NOT NULL,
			filepath    TEXT     NOT NULL,
			opened_at   DATETIME NOT NULL,
			printed_at  DATETIME
		);
		CREATE INDEX IF NOT EXISTS idx_filename
			ON file_metadata (filename, opened_at DESC);
	`)
	return err
}

// RecordOpen stores metadata for a file that was just opened.
func (s *Store) RecordOpen(filename, filePath string) error {
	_, err := s.db.Exec(
		`INSERT INTO file_metadata (filename, filepath, opened_at) VALUES (?, ?, ?)`,
		filename, filePath, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("db: record open: %w", err)
	}
	return nil
}

// LookupByFilename returns the most-recently-opened record matching filename.
// Returns sql.ErrNoRows if no match is found.
func (s *Store) LookupByFilename(filename string) (*FileMetadata, error) {
	row := s.db.QueryRow(`
		SELECT id, filename, filepath, opened_at, printed_at
		FROM file_metadata
		WHERE filename = ?
		ORDER BY opened_at DESC
		LIMIT 1
	`, filename)
	return scanRow(row)
}

// MarkPrinted sets the printed_at timestamp for the record with the given id.
func (s *Store) MarkPrinted(id int64) error {
	_, err := s.db.Exec(
		`UPDATE file_metadata SET printed_at = ? WHERE id = ?`,
		time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("db: mark printed: %w", err)
	}
	return nil
}

// Close releases all database resources.
func (s *Store) Close() error {
	return s.db.Close()
}

func scanRow(row *sql.Row) (*FileMetadata, error) {
	var m FileMetadata
	var printedAt sql.NullTime
	if err := row.Scan(&m.ID, &m.Filename, &m.Filepath, &m.OpenedAt, &printedAt); err != nil {
		return nil, err
	}
	if printedAt.Valid {
		m.PrintedAt = &printedAt.Time
	}
	return &m, nil
}
