package core

import (
	"time"

	"golang.org/x/sys/windows"
)

// WaitForExit waits until process pid has ended, at most timeout.
func WaitForExit(pid int, timeout time.Duration) {
	h, e := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if e != nil {
		return // already gone
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(timeout/time.Millisecond))
}
