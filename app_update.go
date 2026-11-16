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

// CancelUpdateWait calls off an update that is waiting for images to finish.
func (a *App) CancelUpdateWait() { a.service.CancelUpdateWait() }

// InstallUpdate is the one-click update: it returns at once; the download,
// check and restart follow in the background (see core.UpdateNow). When the new
// version has taken over, the app quits and the new version starts it again.
func (a *App) InstallUpdate() error {
	return a.service.UpdateNow(func() {
		a.force.Store(true)
		a.service.StopForExit()
		runtime.Quit(a.ctx)
	})
}
