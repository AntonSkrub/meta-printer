//go:build !linux

package main

import "errors"

var errFileIdentityNotSupported = errors.New("metafilter: file identity is only available on Linux")

func statIdentity(_ string) (uint64, uint64, error) {
	return 0, 0, errFileIdentityNotSupported
}
