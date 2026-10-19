//go:build windows

package core

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

type blob struct {
	Size uint32
	Data *byte
}

var cryptDLL = syscall.NewLazyDLL("crypt32.dll")
var protect = cryptDLL.NewProc("CryptProtectData")
var unprotect = cryptDLL.NewProc("CryptUnprotectData")
var freeMem = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")

func crypt(data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("密钥为空")
	}
	in := blob{uint32(len(data)), &data[0]}
	var out blob
	proc := protect
	if decrypt {
		proc = unprotect
	}
	ok, _, e := proc.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	runtime.KeepAlive(data)
	if ok == 0 {
		return nil, e
	}
	defer freeMem.Call(uintptr(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
func Protect(data []byte) ([]byte, error)   { return crypt(data, false) }
func Unprotect(data []byte) ([]byte, error) { return crypt(data, true) }
