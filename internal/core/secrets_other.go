//go:build !windows

package core

import "errors"

func Protect(data []byte) ([]byte, error) {
	return nil, errors.New("首版密钥存储仅支持 Windows")
}
func Unprotect(data []byte) ([]byte, error) {
	return nil, errors.New("首版密钥存储仅支持 Windows")
}
