//go:build linux

package main

import "golang.org/x/sys/unix"

func statIdentity(path string) (uint64, uint64, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return 0, 0, err
	}

	return stat.Dev, stat.Ino, nil
}
