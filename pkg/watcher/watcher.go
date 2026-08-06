//go:build linux

// Package watcher detects when document files are opened using Linux inotify.
package watcher

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// supportedExts is the set of file extensions the daemon tracks.
var supportedExts = map[string]bool{
	".pdf":  true,
	".docx": true,
	".doc":  true,
	".odt":  true,
	".txt":  true,
	".rtf":  true,
}

// Event represents a file-open detection.
type Event struct {
	Name     string // base filename
	Path     string // full path
	DeviceID uint64 // filesystem device id
	InodeNum uint64 // inode number within the filesystem
}

// Watcher monitors directories via inotify and emits Events when a watched
// file type is opened.
type Watcher struct {
	fd     int
	wds    map[int]string // inotify watch descriptor → directory
	Events chan Event
	Errors chan error
	done   chan struct{}
}

// New creates a new Watcher. Call Add to register directories, then Start.
func New() (*Watcher, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		return nil, err
	}
	return &Watcher{
		fd:     fd,
		wds:    make(map[int]string),
		Events: make(chan Event, 128),
		Errors: make(chan error, 16),
		done:   make(chan struct{}),
	}, nil
}

// Add starts watching path for IN_OPEN events.
func (w *Watcher) Add(path string) error {
	wd, err := unix.InotifyAddWatch(w.fd, path, unix.IN_OPEN)
	if err != nil {
		return err
	}
	w.wds[int(wd)] = path
	return nil
}

// Start launches the event-reading goroutine.
func (w *Watcher) Start() {
	go w.readEvents()
}

// Stop signals the goroutine to exit and releases the inotify file descriptor.
func (w *Watcher) Stop() {
	close(w.done)
	err := unix.Close(w.fd)
	if err != nil {
		select {
		case w.Errors <- fmt.Errorf("watcher: close fd: %w", err):
		default:
		}
	}
}

func (w *Watcher) readEvents() {
	// buf must be large enough for at least one inotify_event + NAME_MAX bytes.
	buf := make([]byte, 4096)
	for {
		n, err := unix.Read(w.fd, buf)
		if err != nil {
			select {
			case <-w.done:
				// Shutdown triggered – expected error from Close(fd).
			case w.Errors <- err:
			}
			return
		}

		offset := 0
		for offset+unix.SizeofInotifyEvent <= n {
			raw := (*unix.InotifyEvent)(unsafe.Pointer(&buf[offset]))
			mask := raw.Mask
			nameLen := int(raw.Len)
			wd := int(raw.Wd)

			name := ""
			end := offset + unix.SizeofInotifyEvent + nameLen
			if nameLen > 0 && end <= n {
				nameBytes := buf[offset+unix.SizeofInotifyEvent : end]
				name = strings.TrimRight(string(nameBytes), "\x00")
			}

			offset += unix.SizeofInotifyEvent + nameLen

			// Only care about opens of regular files (not directories).
			if mask&unix.IN_OPEN == 0 || mask&unix.IN_ISDIR != 0 || name == "" {
				continue
			}

			ext := strings.ToLower(filepath.Ext(name))
			if !supportedExts[ext] {
				continue
			}

			dir := w.wds[wd]
			fullPath := filepath.Join(dir, name)
			ev := Event{Name: name, Path: fullPath}

			if deviceID, inodeNum, err := statIdentity(fullPath); err != nil {
				select {
				case w.Errors <- fmt.Errorf("watcher: stat %q: %w", fullPath, err):
				default:
				}
			} else {
				ev.DeviceID = deviceID
				ev.InodeNum = inodeNum
			}

			select {
			case w.Events <- ev:
			default:
				// Channel full – discard event.
			}
		}
	}
}

func statIdentity(path string) (uint64, uint64, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return 0, 0, err
	}

	return uint64(stat.Dev), stat.Ino, nil
}
