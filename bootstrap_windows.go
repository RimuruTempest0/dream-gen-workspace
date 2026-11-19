//go:build windows

package main

import (
	"golang.org/x/sys/windows"
	"imageworkbench/internal/i18n"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

func fatal(message string) {
	user := syscall.NewLazyDLL("user32.dll")
	body, _ := syscall.UTF16PtrFromString(message)
	title, _ := syscall.UTF16PtrFromString(i18n.AppName(bootLang))
	user.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0x10)
	runtime.KeepAlive(body)
}
func prepareRuntime(path string) error {
	if path == "" {
		return nil
	}
	version := windows.RtlGetVersion()
	if version.BuildNumber >= 22000 {
		return nil
	}
	for _, sid := range []string{"*S-1-15-2-1:(OI)(CI)(RX)", "*S-1-15-2-2:(OI)(CI)(RX)"} {
		cmd := exec.Command("icacls.exe", path, "/grant", sid)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if e := cmd.Run(); e != nil {
			return i18n.Fail("app.boot.icacls", "error", e)
		}
	}
	return nil
}
func claimInstance() (func(), bool, error) {
	name, _ := windows.UTF16PtrFromString(instanceName)
	handle, e := windows.CreateMutex(nil, false, name)
	if e == windows.ERROR_ALREADY_EXISTS {
		windows.CloseHandle(handle)
		u := syscall.NewLazyDLL("user32.dll")
		// The window is titled in the interface language: 生图工作台 or Image Workbench.
		var hwnd uintptr
		for _, name := range []string{i18n.AppName(i18n.ZhCN), i18n.AppName(i18n.En)} {
			title, _ := windows.UTF16PtrFromString(name)
			hwnd, _, _ = u.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(title)))
			runtime.KeepAlive(title)
			if hwnd != 0 {
				break
			}
		}
		if hwnd != 0 {
			u.NewProc("ShowWindow").Call(hwnd, 9)
			u.NewProc("SetForegroundWindow").Call(hwnd)
		}
		return func() {}, false, nil
	}
	if e != nil {
		return func() {}, false, e
	}
	return func() { windows.CloseHandle(handle) }, true, nil
}
func openDirectory(path string) error {
	path, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	info, e := os.Stat(path)
	if e != nil {
		return e
	}
	if !info.IsDir() {
		return i18n.Fail("app.openData.notDirectory")
	}
	verb, _ := windows.UTF16PtrFromString("open")
	target, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	if e = windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL); e != nil {
		return i18n.Fail("app.openData.failed", "error", e)
	}
	return nil
}
