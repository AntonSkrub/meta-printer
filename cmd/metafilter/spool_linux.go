//go:build linux

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openTrustedSpoolFile(dir, name string) (*os.File, error) {
	// O_PATH requires directory search permission, not permission to list it.
	dirFD, err := unix.Open(dir, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open spool directory: %w", err)
	}
	defer unix.Close(dirFD)

	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat spool file: %w", err)
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("spool input is not a regular file")
	}
	return f, nil
}
