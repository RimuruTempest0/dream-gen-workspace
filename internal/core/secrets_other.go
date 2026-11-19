//go:build !windows

package core

import "errors"

func Protect(data []byte) ([]byte, error) {
	return nil, errors.New("key storage works only on Windows")
}
func Unprotect(data []byte) ([]byte, error) {
	return nil, errors.New("key storage works only on Windows")
}
