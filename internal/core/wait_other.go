//go:build !windows

package core

import (
	"os"
	"syscall"
	"time"
)

// WaitForExit waits until process pid has ended, at most timeout.
func WaitForExit(pid int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		p, e := os.FindProcess(pid)
		if e != nil || p.Signal(syscall.Signal(0)) != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
