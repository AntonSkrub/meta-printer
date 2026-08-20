//go:build !linux

// Package watcher provides a stub implementation for non-Linux platforms.
// Only Linux (inotify) is supported at runtime; this file lets the module
// compile on other platforms.
package watcher

import "errors"

// ErrNotSupported is returned on platforms where watching is unavailable.
var ErrNotSupported = errors.New("watcher: inotify is only available on Linux")

// Event represents a file-open detection.
type Event struct {
	Name     string
	Path     string
	DeviceID uint64
	InodeNum uint64
}

// Watcher is a no-op implementation for non-Linux platforms.
type Watcher struct {
	Events chan Event
	Errors chan error
}

// New returns ErrNotSupported on non-Linux platforms.
func New() (*Watcher, error) {
	return nil, ErrNotSupported
}

// Add is a no-op.
func (w *Watcher) Add(_ string) error { return ErrNotSupported }

// Start is a no-op.
func (w *Watcher) Start() {}

// Stop is a no-op.
func (w *Watcher) Stop() {}
