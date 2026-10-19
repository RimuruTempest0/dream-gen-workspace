//go:build windows

package core

import "golang.org/x/sys/windows"

var procAllowSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")

// allowBrowserForeground lets the helper bring its verification window to the front.
func allowBrowserForeground(pid int) { procAllowSetForegroundWindow.Call(uintptr(pid)) }
