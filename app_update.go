package main

import (
	"imageworkbench/internal/core"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) UpdateStatus() core.UpdateStatus { return a.service.UpdateStatus() }
func (a *App) CheckForUpdate() (core.UpdateStatus, error) {
	return a.service.CheckForUpdate()
}
func (a *App) SetUpdateAutoCheck(on bool) error { return a.service.SetUpdateAutoCheck(on) }
func (a *App) DownloadUpdate() (core.UpdateStatus, error) {
	return a.service.DownloadUpdate()
}

// InstallUpdate hands over to the downloaded version and quits; the new version
// replaces the program files and starts again.
func (a *App) InstallUpdate() error {
	if e := a.service.StartUpdate(); e != nil {
		return e
	}
	a.force.Store(true)
	a.service.StopForExit()
	runtime.Quit(a.ctx)
	return nil
}
