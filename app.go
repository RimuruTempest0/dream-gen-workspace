package main

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"imageworkbench/internal/core"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

type App struct {
	ctx        context.Context
	service    *core.Service
	force      atomic.Bool
	configRoot string
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.service.Start(func() { runtime.EventsEmit(ctx, "tasks:changed"); runtime.EventsEmit(ctx, "agent:changed") })
}
func (a *App) shutdown(ctx context.Context) { a.service.Close() }
func (a *App) beforeClose(ctx context.Context) bool {
	if a.force.Load() {
		return false
	}
	s, e := a.service.Dashboard()
	if e == nil && s.Active > 0 {
		runtime.EventsEmit(ctx, "app:close-requested")
		return true
	}
	return false
}
func (a *App) QuitNow() { runtime.Quit(a.ctx) }
func (a *App) ForceQuit() {
	a.force.Store(true)
	a.service.StopAllQueued()
	a.service.StopForExit()
	runtime.Quit(a.ctx)
}
func (a *App) Dashboard() (core.Summary, error) { return a.service.Dashboard() }
func (a *App) OpenDataDirectory() error         { return openDirectory(a.service.Root()) }
func (a *App) ChooseDataDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择新的保存位置（空文件夹）", DefaultDirectory: a.service.Root()})
}
func (a *App) ChangeDataDirectory(path string) (core.Summary, error) {
	if e := a.service.ChangeDataDirectory(path, func(target string) error { return saveDataPath(a.configRoot, target) }); e != nil {
		return core.Summary{}, e
	}
	return a.service.Dashboard()
}
func (a *App) ListConnections() ([]core.Connection, error) { return a.service.ListConnections() }
func (a *App) SaveConnection(v core.ConnectionInput) (core.Connection, error) {
	return a.service.SaveConnection(v)
}
func (a *App) TestConnection(v core.ConnectionInput) (core.TestResult, error) {
	return a.service.TestConnection(v)
}
func (a *App) DeleteConnection(id string) error            { return a.service.DeleteConnection(id) }
func (a *App) Submit(v core.GenerateInput) (string, error) { return a.service.Submit(v) }
func (a *App) ListTasks(status string, offset, limit int) (core.Page, error) {
	return a.service.ListTasks(status, offset, limit)
}
func (a *App) ListRecentBatches(limit int) ([]core.RecentBatch, error) {
	return a.service.ListRecentBatches(limit)
}
func (a *App) GetExportAsset(id string) (core.Asset, error) { return a.service.AssetForExport(id) }
func (a *App) CancelConnectionTest(id string)               { a.service.CancelConnectionTest(id) }
func (a *App) GetTask(id string) (core.Task, error)         { return a.service.GetTask(id) }
func (a *App) StopBatch(id string) error                    { return a.service.StopBatch(id) }
func (a *App) CancelTask(id string) error                   { return a.service.CancelTask(id) }
func (a *App) Retry(id string) (string, error)              { return a.service.Retry(id) }
func (a *App) RetryDownload(id string) error                { return a.service.RetryDownload(id) }
func (a *App) ResumeBlocked(id string) error                { return a.service.ResumeBlocked(id) }
func (a *App) GetDraft() (core.GenerateInput, error)        { return a.service.GetDraft() }
func (a *App) SetDraft(v core.GenerateInput) error          { return a.service.SetDraft(v) }
func (a *App) ListAssets(project string, offset, limit int) (core.AssetPage, error) {
	return a.service.ListAssets(project, offset, limit)
}
func (a *App) ImportImage(name, data string) (core.Asset, error) {
	return a.service.ImportImage(name, data)
}
func (a *App) ReadAsset(id string, thumbnail bool) (string, error) {
	return a.service.ReadAsset(id, thumbnail)
}
func (a *App) UpdateAsset(id, project string, hidden bool) error {
	return a.service.UpdateAsset(id, project, hidden)
}
func (a *App) ImportFiles() ([]core.Asset, error) {
	paths, e := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: "添加参考图", Filters: []runtime.FileFilter{{DisplayName: "图片", Pattern: "*.png;*.jpg;*.jpeg;*.webp"}}})
	if e != nil {
		return nil, e
	}
	return a.importPaths(paths)
}
func (a *App) importPaths(paths []string) ([]core.Asset, error) {
	out := []core.Asset{}
	if len(paths) > 4 {
		return nil, errors.New("一次最多导入 4 张参考图")
	}
	for _, path := range paths {
		info, e := os.Stat(path)
		if e != nil {
			return out, e
		}
		if info.Size() > core.MaxImageBytes {
			return out, errors.New("图片超过 32 MB")
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return out, e
		}
		asset, e := a.service.ImportImage(filepath.Base(path), base64.StdEncoding.EncodeToString(b))
		if e != nil {
			return out, e
		}
		out = append(out, asset)
	}
	return out, nil
}
func (a *App) ExportAsset(id, name string) (bool, error) {
	asset, e := a.service.AssetForExport(id)
	if e != nil {
		return false, e
	}
	path, e := a.ChooseExportPath(filepath.Base(asset.Path))
	if e != nil || path == "" {
		return false, e
	}
	return true, a.service.ExportAsset(id, path)
}
func (a *App) ChooseExportPath(name string) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		ext = ".png"
	}
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" && ext != ".zip" && ext != ".csv" {
		return "", errors.New("导出格式不受支持")
	}
	title := "保存图片"
	label := "图片"
	if ext == ".zip" {
		title = "保存套图 ZIP"
		label = "ZIP 套图"
	}
	if ext == ".csv" {
		title = "保存调用记录 CSV"
		label = "CSV 表格"
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: title, DefaultFilename: filepath.Base(name), Filters: []runtime.FileFilter{{DisplayName: label, Pattern: "*" + ext}}})
}
func (a *App) ExportAssetToPath(id, path string) error        { return a.service.ExportAsset(id, path) }
func (a *App) ExportBatchToPath(id, path string) (int, error) { return a.service.ExportBatch(id, path) }
func (a *App) ExportAssetsToPath(ids []string, path string) (int, error) {
	return a.service.ExportAssets(ids, path)
}
func (a *App) UpdateAssets(ids []string, project string, hidden bool) (int, error) {
	return a.service.UpdateAssets(ids, project, hidden)
}
func (a *App) ListProjects() ([]core.Project, error)               { return a.service.ListProjects() }
func (a *App) SaveProject(v core.Project) (core.Project, error)    { return a.service.SaveProject(v) }
func (a *App) DeleteProject(id string) error                       { return a.service.DeleteProject(id) }
func (a *App) ListTemplates() ([]core.Template, error)             { return a.service.ListTemplates() }
func (a *App) SaveTemplate(v core.Template) (core.Template, error) { return a.service.SaveTemplate(v) }
func (a *App) DeleteTemplate(id string) error                      { return a.service.DeleteTemplate(id) }
func (a *App) Backup() (bool, error) {
	path, e := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "保存数据备份", DefaultFilename: "ImageWorkbench-backup.zip", Filters: []runtime.FileFilter{{DisplayName: "ZIP 备份", Pattern: "*.zip"}}})
	if e != nil || path == "" {
		return false, e
	}
	return true, a.service.CreateBackup(path)
}
func (a *App) Restore() (bool, error) {
	path, e := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择备份", Filters: []runtime.FileFilter{{DisplayName: "ZIP 备份", Pattern: "*.zip"}}})
	if e != nil || path == "" {
		return false, e
	}
	return true, a.service.RestoreBackup(path)
}

func (a *App) ListLogs(level, source, keyword string, offset, limit int) (core.LogPage, error) {
	return a.service.ListLogs(level, source, keyword, offset, limit)
}
