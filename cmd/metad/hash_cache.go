package main

import (
	"fmt"
	"os"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/rs/zerolog/log"
)

type cachedFileHash struct {
	info       os.FileInfo
	hash       string
	err        error
	retryAfter time.Time
}

type fileHashCache struct {
	files     map[string]cachedFileHash
	titles    map[string]cachedPDFTitle
	compute   func(string) (string, error)
	readTitle func(string) (string, error)
}

type cachedPDFTitle struct {
	info  os.FileInfo
	title string
	err   error
}

func newFileHashCache() *fileHashCache {
	return &fileHashCache{
		files: make(map[string]cachedFileHash), titles: make(map[string]cachedPDFTitle),
		compute: hashFile, readTitle: readPDFTitle,
	}
}

func sameFileState(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() &&
		a.Mode() == b.Mode() && a.ModTime().Equal(b.ModTime()) &&
		fileChangeTime(a).Equal(fileChangeTime(b))
}

func (c *fileHashCache) hash(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		delete(c.files, path)
		return "", fmt.Errorf("stat: %w", err)
	}

	if cached, ok := c.files[path]; ok && sameFileState(info, cached.info) {
		if cached.err == nil || time.Now().Before(cached.retryAfter) {
			return cached.hash, cached.err
		}
	}

	// Opening a watched file generates another IN_OPEN. Cache the result before
	// that event is processed so hashing cannot trigger itself indefinitely.
	hash, hashErr := c.compute(path)
	after, err := os.Stat(path)
	if err == nil && sameFileState(info, after) {
		entry := cachedFileHash{info: after, hash: hash, err: hashErr}
		if hashErr != nil {
			entry.retryAfter = time.Now().Add(time.Second)
		}
		c.files[path] = entry
	} else {
		delete(c.files, path)
	}
	return hash, hashErr
}

func (c *fileHashCache) title(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		delete(c.titles, path)
		return "", fmt.Errorf("stat: %w", err)
	}
	if cached, ok := c.titles[path]; ok && sameFileState(info, cached.info) {
		return cached.title, cached.err
	}
	title, titleErr := c.readTitle(path)
	after, err := os.Stat(path)
	if err == nil && sameFileState(info, after) {
		c.titles[path] = cachedPDFTitle{info: after, title: title, err: titleErr}
	} else {
		delete(c.titles, path)
		return "", fmt.Errorf("PDF changed while reading title")
	}
	return title, titleErr
}

func readPDFTitle(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- path is supplied by the watcher.
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Error().Err(err).Str("path", path).Msg("metad: close PDF")
		}
	}()
	ctx, err := api.ReadAndValidate(f, model.NewDefaultConfiguration())
	if err != nil {
		return "", fmt.Errorf("parse PDF: %w", err)
	}
	return ctx.Title, nil
}
