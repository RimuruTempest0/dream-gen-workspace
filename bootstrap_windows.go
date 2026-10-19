//go:build windows

package main

import (
	"fmt"
	"golang.org/x/sys/windows"
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
	title, _ := syscall.UTF16PtrFromString("生图工作台")
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
			return fmt.Errorf("无法设置 runtime 目录读取权限：%w", e)
		}
	}
	return nil
}
func claimInstance() (func(), bool, error) {
	name, _ := windows.UTF16PtrFromString(instanceName)
	handle, e := windows.CreateMutex(nil, false, name)
	if e == windows.ERROR_ALREADY_EXISTS {
		windows.CloseHandle(handle)
		title, _ := windows.UTF16PtrFromString("生图工作台")
		u := syscall.NewLazyDLL("user32.dll")
		hwnd, _, _ := u.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(title)))
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
		return fmt.Errorf("数据位置不是目录")
	}
	verb, _ := windows.UTF16PtrFromString("open")
	target, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	if e = windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL); e != nil {
		return fmt.Errorf("无法打开数据目录：%w", e)
	}
	return nil
}
