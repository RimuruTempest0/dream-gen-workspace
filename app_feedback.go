package main

import (
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"imageworkbench/internal/core"
)

// 问题反馈 on a 套图 and its 反馈包 (#14).

func (a *App) GetSuiteFeedback(runID string) (core.SuiteFeedback, error) {
	return a.service.GetSuiteFeedback(runID)
}
func (a *App) SaveSuiteFeedback(v core.SuiteFeedback) (core.SuiteFeedback, error) {
	return a.service.SaveSuiteFeedback(v)
}
// GetFeedbackImages lists the suite's cards and versions for 逐张核验.
func (a *App) GetFeedbackImages(runID string) ([]core.FeedbackImage, error) {
	return a.service.GetFeedbackImages(runID)
}
func (a *App) PreviewFeedbackPackage(runID string) (core.FeedbackPackagePreview, error) {
	return a.service.PreviewFeedbackPackage(runID)
}

// ChooseFeedbackPath asks where to save the 反馈包: the desktop by default.
func (a *App) ChooseFeedbackPath() (string, error) {
	name := a.msg("app.feedback.filename", "time", time.Now().Format("20060102-1504"))
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: a.msg("app.dialog.feedback"), DefaultDirectory: desktopDir(), DefaultFilename: name, Filters: []runtime.FileFilter{{DisplayName: a.msg("app.filter.feedback"), Pattern: "*.zip"}}})
}

// ExportFeedbackPackageToPath writes the suite's 反馈包 (no keys or line addresses).
func (a *App) ExportFeedbackPackageToPath(runID, path string) error {
	return a.service.ExportFeedbackPackage(runID, path)
}
