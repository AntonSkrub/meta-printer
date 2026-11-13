//go:build !linux

package main

import (
	"errors"
	"os"
)

func openTrustedSpoolFile(_, _ string) (*os.File, error) {
	return nil, errors.New("opening CUPS spool files is only supported on Linux")
}
