//go:build linux

// Package watcher detects when document files are opened using Linux inotify.
package watcher

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// supportedExts is the set of file extensions the daemon tracks.
var supportedExts = map[string]bool{
	".pdf": true,
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
	mu     sync.Mutex
	wg     sync.WaitGroup
	wds    map[int]string // inotify watch descriptor → directory
	Events chan Event
	Errors chan error
	done   chan struct{}
}

// New creates a new Watcher. Call Add to register directories, then Start.
func New() (*Watcher, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
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

// Add watches path and its existing subdirectories without following symlinks.
func (w *Watcher) Add(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return w.addTree(path)
}

func (w *Watcher) addTree(root string) error {
	var failures []error
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			failures = append(failures, fmt.Errorf("watch %q: %w", path, walkErr))
			return nil
		}
		if !entry.IsDir() {
			if path == root {
				failures = append(failures, fmt.Errorf("watch %q: not a directory or is a symlink", path))
			}
			return nil
		}
		w.mu.Lock()
		wd, err := unix.InotifyAddWatch(w.fd, path, unix.IN_OPEN|unix.IN_CREATE|unix.IN_MOVED_TO|
			unix.IN_MOVED_FROM|unix.IN_DELETE|unix.IN_DELETE_SELF|unix.IN_MOVE_SELF|unix.IN_ONLYDIR|unix.IN_DONT_FOLLOW)
		if err == nil {
			w.wds[wd] = path
		}
		w.mu.Unlock()
		if err != nil {
			failures = append(failures, fmt.Errorf("watch %q: %w", path, err))
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (w *Watcher) removeTree(root string) {
	w.mu.Lock()
	var failures []error
	for wd, path := range w.wds {
		if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
			continue
		}
		delete(w.wds, wd)
		if _, err := unix.InotifyRmWatch(w.fd, uint32(wd)); err != nil && !errors.Is(err, unix.EINVAL) {
			failures = append(failures, fmt.Errorf("unwatch %q: %w", path, err))
		}
	}
	w.mu.Unlock()
	for _, err := range failures {
		w.reportError(err)
	}
}

func (w *Watcher) reportError(err error) {
	select {
	case w.Errors <- err:
	default:
		fmt.Fprintln(os.Stderr, err)
	}
}

// Start launches the event-reading goroutine.
func (w *Watcher) Start() {
	w.wg.Add(1)
	go w.readEvents()
}

// Stop signals the goroutine to exit and releases the inotify file descriptor.
func (w *Watcher) Stop() {
	close(w.done)
	w.wg.Wait()
	err := unix.Close(w.fd)
	if err != nil {
		w.reportError(fmt.Errorf("watcher: close fd: %w", err))
	}
}

func (w *Watcher) readEvents() {
	defer w.wg.Done()
	// buf must be large enough for at least one inotify_event + NAME_MAX bytes.
	buf := make([]byte, 4096)
	for {
		select {
		case <-w.done:
			return
		default:
		}
		fds := []unix.PollFd{{Fd: int32(w.fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(fds, 200)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			w.reportError(fmt.Errorf("watcher: poll: %w", err))
			return
		}
		if ready == 0 {
			continue
		}
		if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			w.reportError(fmt.Errorf("watcher: unexpected poll events: %d", fds[0].Revents))
			return
		}
		n, err := unix.Read(w.fd, buf)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			select {
			case <-w.done:
				// Shutdown triggered – expected error from Close(fd).
			default:
				w.reportError(err)
			}
			return
		}

		offset := 0
		for offset+unix.SizeofInotifyEvent <= n {
			// #nosec G103 -- Safe: the buffer bounds are verified before casting to
			// unix.InotifyEvent, matching the Linux inotify event layout
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

			if mask&unix.IN_Q_OVERFLOW != 0 {
				w.reportError(errors.New("watcher: inotify queue overflow; events were lost, restart metad"))
				continue
			}

			w.mu.Lock()
			dir, watched := w.wds[wd]
			if mask&unix.IN_IGNORED != 0 {
				delete(w.wds, wd)
			}
			w.mu.Unlock()
			if !watched {
				continue
			}
			if mask&unix.IN_IGNORED != 0 {
				continue
			}
			if mask&(unix.IN_DELETE_SELF|unix.IN_MOVE_SELF) != 0 {
				w.removeTree(dir)
				w.reportError(fmt.Errorf("watcher: watched directory %q moved or removed", dir))
				continue
			}
			if mask&unix.IN_ISDIR != 0 && name != "" {
				path := filepath.Join(dir, name)
				if mask&(unix.IN_MOVED_FROM|unix.IN_DELETE) != 0 {
					w.removeTree(path)
				}
				if mask&(unix.IN_CREATE|unix.IN_MOVED_TO) != 0 {
					if err := w.addTree(path); err != nil {
						w.reportError(err)
					}
				}
				continue
			}
			if mask&unix.IN_OPEN == 0 || name == "" {
				continue
			}
			ext := strings.ToLower(filepath.Ext(name))
			if !supportedExts[ext] {
				continue
			}

			fullPath := filepath.Join(dir, name)
			ev := Event{Name: name, Path: fullPath}

			if deviceID, inodeNum, err := statIdentity(fullPath); err != nil {
				w.reportError(fmt.Errorf("watcher: stat %q: %w", fullPath, err))
			} else {
				ev.DeviceID = deviceID
				ev.InodeNum = inodeNum
			}

			select {
			case w.Events <- ev:
			default:
				w.reportError(fmt.Errorf("watcher: event queue full; lost open event for %q", fullPath))
			}
		}
	}
}

func statIdentity(path string) (uint64, uint64, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return 0, 0, err
	}

	return stat.Dev, stat.Ino, nil
}
