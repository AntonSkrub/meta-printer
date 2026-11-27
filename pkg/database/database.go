package database

// Package db provides a SQLite-backed store for file-open metadata collected
// by the metadata daemon and consumed by the CUPS print filter.

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// FileMetadata holds the information recorded when a file is opened.
type FileMetadata struct {
	ID        int64
	Filename  string
	Filepath  string
	FileHash  string
	DeviceID  uint64
	InodeNum  uint64
	OpenedAt  time.Time
	PrintedAt *time.Time
}

// Store is a thread-safe metadata store backed by SQLite.
type Store struct {
	db *sql.DB
}

// ErrAmbiguousJobTitle means multiple files match a job title.
var ErrAmbiguousJobTitle = errors.New("job title matches multiple files")

// ErrAmbiguousFileHash means multiple file paths match the same content hash.
var ErrAmbiguousFileHash = errors.New("file hash matches multiple files")

// New opens (or creates) the SQLite database at path, creating any parent
// directories as needed, and runs the schema migration.
func New(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("db: create parent directory: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("db: resolve path: %w", err)
	}
	uriPath := filepath.ToSlash(absPath)
	if runtime.GOOS == "windows" {
		uriPath = "/" + uriPath
	}
	dsn := (&url.URL{Scheme: "file", Path: uriPath, RawQuery: "_pragma=busy_timeout%3D5000"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}

	// Single-writer SQLite; enable WAL for better concurrency.
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "db: close: %v\n", closeErr)
		}
		return nil, fmt.Errorf("db: enable WAL: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		if closeErr := s.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "db: close: %v\n", closeErr)
		}
		return nil, fmt.Errorf("db: migrate: %w", err)
	}
	return s, nil
}

// RecordOpen stores metadata for a file that was just opened.
func (s *Store) RecordOpen(filename, filePath, fileHash, title string, deviceID, inodeNum uint64) error {
	var hashValue any
	if fileHash != "" {
		hashValue = fileHash
	}

	var devValue any
	if deviceID != 0 {
		devValue = strconv.FormatUint(deviceID, 10)
	}

	var inodeValue any
	if inodeNum != 0 {
		inodeValue = strconv.FormatUint(inodeNum, 10)
	}

	_, err := s.db.Exec(
		`INSERT INTO file_metadata (filename, filepath, file_hash, title, dev_id, inode_num, opened_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		filename, filePath, hashValue, title, devValue, inodeValue, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("db: record open: %w", err)
	}
	return nil
}

// DeleteOpenedBefore deletes records opened before cutoff, conservatively
// retaining the cutoff second to accommodate timestamp formats in old databases.
func (s *Store) DeleteOpenedBefore(cutoff time.Time) (int64, error) {
	result, err := s.db.Exec(
		`DELETE FROM file_metadata WHERE substr(opened_at, 1, 19) < ?`,
		cutoff.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return 0, fmt.Errorf("db: delete expired records: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("db: count expired records: %w", err)
	}
	return count, nil
}

// LookupByJobTitle matches the latest record per path by filename or PDF title.
// Multiple matching paths are ambiguous and must not be used for attribution.
func (s *Store) LookupByJobTitle(title string) (*FileMetadata, error) {
	if title == "" {
		return nil, sql.ErrNoRows
	}
	rows, err := s.db.Query(`
		SELECT f.id, f.filename, f.filepath, f.file_hash, f.dev_id, f.inode_num, f.opened_at, f.printed_at
		FROM file_metadata f
		WHERE (f.filename = ? OR f.title = ?)
		  AND NOT EXISTS (SELECT 1 FROM file_metadata newer WHERE newer.filepath = f.filepath AND newer.id > f.id)
		ORDER BY f.id DESC LIMIT 2`, filepath.Base(title), title)
	if err != nil {
		return nil, fmt.Errorf("db: lookup job title: %w", err)
	}
	return uniqueRecord(rows, ErrAmbiguousJobTitle)
}

func uniqueRecord(rows *sql.Rows, ambiguous error) (*FileMetadata, error) {
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "db: close rows: %v\n", err)
		}
	}()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, sql.ErrNoRows
	}
	record, err := scanRow(rows)
	if err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, ambiguous
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return record, nil
}

// LookupByDevInode returns the most-recently-opened record matching device and inode.
// Returns sql.ErrNoRows if no match is found.
func (s *Store) LookupByDevInode(deviceID, inodeNum uint64) (*FileMetadata, error) {
	row := s.db.QueryRow(`
		SELECT id, filename, filepath, file_hash, dev_id, inode_num, opened_at, printed_at
		FROM file_metadata
		WHERE dev_id = ? AND inode_num = ?
		ORDER BY opened_at DESC
		LIMIT 1
	`, strconv.FormatUint(deviceID, 10), strconv.FormatUint(inodeNum, 10))
	return scanRow(row)
}

// LookupByFileHash returns a match only when one path has this hash in its
// latest record. Multiple matching paths return ErrAmbiguousFileHash.
func (s *Store) LookupByFileHash(fileHash string) (*FileMetadata, error) {
	if fileHash == "" {
		return nil, sql.ErrNoRows
	}
	rows, err := s.db.Query(`
		SELECT f.id, f.filename, f.filepath, f.file_hash, f.dev_id, f.inode_num, f.opened_at, f.printed_at
		FROM file_metadata f
		WHERE f.file_hash = ?
		  AND NOT EXISTS (SELECT 1 FROM file_metadata newer WHERE newer.filepath = f.filepath AND newer.id > f.id)
		ORDER BY f.id DESC LIMIT 2`, fileHash)
	if err != nil {
		return nil, fmt.Errorf("db: lookup file hash: %w", err)
	}
	return uniqueRecord(rows, ErrAmbiguousFileHash)
}

// LookupByFilename returns the most-recently-opened record matching filename.
// Returns sql.ErrNoRows if no match is found.
func (s *Store) LookupByFilename(filename string) (*FileMetadata, error) {
	row := s.db.QueryRow(`
		SELECT id, filename, filepath, file_hash, dev_id, inode_num, opened_at, printed_at
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

// migrate creates the schema if it does not already exist.
func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS file_metadata (
			id          INTEGER  PRIMARY KEY AUTOINCREMENT,
			filename    TEXT     NOT NULL,
			filepath    TEXT     NOT NULL,
			file_hash   TEXT,
			title       TEXT,
			dev_id      TEXT,
			inode_num   TEXT,
			opened_at   DATETIME NOT NULL,
			printed_at  DATETIME
		);
		CREATE INDEX IF NOT EXISTS idx_filename
			ON file_metadata (filename, opened_at DESC);
	`)
	if err != nil {
		return err
	}

	hasHash, err := s.columnExists("file_metadata", "file_hash")
	if err != nil {
		return err
	}
	if !hasHash {
		if _, err := s.db.Exec(`ALTER TABLE file_metadata ADD COLUMN file_hash TEXT`); err != nil {
			return err
		}
	}

	hasDevID, err := s.columnExists("file_metadata", "dev_id")
	if err != nil {
		return err
	}
	if !hasDevID {
		if _, err := s.db.Exec(`ALTER TABLE file_metadata ADD COLUMN dev_id TEXT`); err != nil {
			return err
		}
	}

	hasInodeNum, err := s.columnExists("file_metadata", "inode_num")
	if err != nil {
		return err
	}
	if !hasInodeNum {
		if _, err := s.db.Exec(`ALTER TABLE file_metadata ADD COLUMN inode_num TEXT`); err != nil {
			return err
		}
	}

	hasTitle, err := s.columnExists("file_metadata", "title")
	if err != nil {
		return err
	}
	if !hasTitle {
		if _, err := s.db.Exec(`ALTER TABLE file_metadata ADD COLUMN title TEXT`); err != nil {
			// Another process may have added the column after our check.
			exists, checkErr := s.columnExists("file_metadata", "title")
			if checkErr != nil {
				return fmt.Errorf("db: check title column after migration failure: %w", checkErr)
			}
			if !exists {
				return err
			}
		}
	}

	if _, err := s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_file_hash
			ON file_metadata (file_hash, opened_at DESC)
	`); err != nil {
		return err
	}

	if _, err := s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_dev_inode
			ON file_metadata (dev_id, inode_num, opened_at DESC)
	`); err != nil {
		return err
	}

	if _, err := s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_title ON file_metadata (title);
		CREATE INDEX IF NOT EXISTS idx_filepath_id ON file_metadata (filepath, id);
	`); err != nil {
		return err
	}

	return err
}

func scanRow(row interface{ Scan(...any) error }) (*FileMetadata, error) {
	var m FileMetadata
	var fileHash sql.NullString
	var deviceID sql.NullString
	var inodeNum sql.NullString
	var printedAt sql.NullTime
	if err := row.Scan(&m.ID, &m.Filename, &m.Filepath, &fileHash, &deviceID, &inodeNum, &m.OpenedAt, &printedAt); err != nil {
		return nil, err
	}
	if fileHash.Valid {
		m.FileHash = fileHash.String
	}
	if deviceID.Valid {
		if value, err := strconv.ParseUint(deviceID.String, 10, 64); err == nil {
			m.DeviceID = value
		}
	}
	if inodeNum.Valid {
		if value, err := strconv.ParseUint(inodeNum.String, 10, 64); err == nil {
			m.InodeNum = value
		}
	}
	if printedAt.Valid {
		m.PrintedAt = &printedAt.Time
	}
	return &m, nil
}

func (s *Store) columnExists(tableName, columnName string) (bool, error) {
	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "db: close rows: %v\n", err)
		}
	}()

	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}
