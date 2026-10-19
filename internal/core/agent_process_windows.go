//go:build windows

package core

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"syscall"
	"unsafe"
)

func configureAgentCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func attachAgentProcess(cmd *exec.Cmd) (func(), error) {
	job, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		return nil, e
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if e != nil {
		windows.CloseHandle(job)
		return nil, e
	}
	p, e := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if e != nil {
		windows.CloseHandle(job)
		return nil, e
	}
	defer windows.CloseHandle(p)
	if e = windows.AssignProcessToJobObject(job, p); e != nil {
		windows.CloseHandle(job)
		return nil, e
	}
	return func() { windows.CloseHandle(job) }, nil
}
