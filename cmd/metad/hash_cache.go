package main

import (
	"fmt"
	"os"
	"time"
)

type cachedFileHash struct {
	info       os.FileInfo
	hash       string
	err        error
	retryAfter time.Time
}

type fileHashCache struct {
	files   map[string]cachedFileHash
	compute func(string) (string, error)
}

func newFileHashCache() *fileHashCache {
	return &fileHashCache{files: make(map[string]cachedFileHash), compute: hashFile}
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
