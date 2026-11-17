//go:build !linux

package main

import (
	"os"
	"time"
)

func fileChangeTime(_ os.FileInfo) time.Time {
	return time.Time{}
}
