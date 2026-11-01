// Command admin is ImageWorkbenchAdmin.exe: the provider's own tool for customers,
// software IDs, activation codes, the key pool and customer packages. It is never
// shipped to customers.
package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"imageworkbench/internal/admin"
	"imageworkbench/internal/core"
)

//go:embed all:dist
var assets embed.FS

// dataDir is kept apart from any customer program: %LOCALAPPDATA%\ImageWorkbenchAdmin,
// or IMAGEWORKBENCH_ADMIN_DATA (tests use an isolated folder).
func dataDir() string {
	if d := os.Getenv("IMAGEWORKBENCH_ADMIN_DATA"); d != "" {
		return d
	}
	base, e := os.UserCacheDir()
	if e != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "ImageWorkbenchAdmin")
}

// App is bound to the admin frontend (window.go.main.App).
type App struct {
	ctx      context.Context
	a        *admin.Admin
	building sync.Mutex
	builder  admin.Builder
}

func main() {
	a, e := admin.Open(dataDir())
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if _, e = a.EnsureSigningKey(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	app := &App{a: a, builder: builder()}
	app.detectRepo()
	e = wails.Run(&options.App{
		Title: "ImageWorkbench 管理工具", Width: 1360, Height: 900, MinWidth: 800, MinHeight: 600,
		BackgroundColour: &options.RGBA{R: 245, G: 246, B: 248, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        func(ctx context.Context) { app.ctx = ctx },
		Bind:             []interface{}{app},
		// The system WebView2 runtime by default; IMAGEWORKBENCH_ADMIN_WEBVIEW may
		// point at a fixed runtime folder instead.
		Windows: &windows.Options{WebviewBrowserPath: os.Getenv("IMAGEWORKBENCH_ADMIN_WEBVIEW"), WebviewUserDataPath: filepath.Join(dataDir(), "webview"), Theme: windows.Light},
	})
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
	}
}

// detectRepo finds the ImageWorkbench source folder above the program (the tool
// is built into release\admin) when none is set yet.
func (x *App) detectRepo() {
	// Desktop QA points at an isolated stand-in source folder.
	if dir := os.Getenv("IMAGEWORKBENCH_ADMIN_REPO"); dir != "" {
		x.a.SetRepoRoot(dir)
		return
	}
	if x.a.State().RepoRoot != "" {
		return
	}
	exe, e := os.Executable()
	if e != nil {
		return
	}
	for dir := filepath.Dir(exe); ; dir = filepath.Dir(dir) {
		if _, e := os.Stat(filepath.Join(dir, "scripts", "build.ps1")); e == nil {
			x.a.SetRepoRoot(dir)
			return
		}
		if filepath.Dir(dir) == dir {
			return
		}
	}
}

func (x *App) State() admin.State                                    { return x.a.State() }
func (x *App) ToolVersion() string                                   { return admin.ToolVersion }
func (x *App) SaveUpstream(u admin.Upstream) (admin.Upstream, error) { return x.a.SaveUpstream(u) }
func (x *App) DeleteUpstream(id string) error                        { return x.a.DeleteUpstream(id) }
func (x *App) ImportKeys(upstreamID, use, text, note string) (admin.ImportResult, error) {
	return x.a.ImportKeys(upstreamID, use, text, note)
}
func (x *App) SetKeyNote(id, note string) error              { return x.a.SetKeyNote(id, note) }
func (x *App) DisableKey(id, reason string) error            { return x.a.DisableKey(id, reason) }
func (x *App) EnableKey(id string) error                     { return x.a.EnableKey(id) }
func (x *App) DisableKeys(ids []string, reason string) error { return x.a.DisableKeys(ids, reason) }
func (x *App) DeleteKeys(ids []string) error                 { return x.a.DeleteKeys(ids) }
func (x *App) SetKeyUse(id, use string) error                { return x.a.SetKeyUse(id, use) }
func (x *App) RevealCode(customerID string) (string, error)  { return x.a.RevealCode(customerID) }
func (x *App) CreateCustomer(name string, trial int) (admin.Customer, error) {
	return x.a.CreateCustomer(name, trial)
}
func (x *App) SetCustomerNotes(id, plan, quota string, trial int) error {
	return x.a.SetCustomerNotes(id, plan, quota, trial)
}
func (x *App) DeleteCustomer(id string) error                  { return x.a.DeleteCustomer(id) }
func (x *App) AddEvent(id, kind, text string) error            { return x.a.AddEvent(id, kind, text) }
func (x *App) AutoAssign(id string) ([]string, error)          { return x.a.AutoAssign(id) }
func (x *App) MarkPaid(id, note string) error                  { return x.a.MarkPaid(id, note) }
func (x *App) AssignKey(id, slot, keyID string) error          { return x.a.AssignKey(id, slot, keyID) }
func (x *App) UnassignKey(id, slot string) error               { return x.a.UnassignKey(id, slot) }
func (x *App) SetImageBackups(id string, order []string) error { return x.a.SetImageBackups(id, order) }

// VerifyLLM makes a few short, billable LLM calls; the UI asks first.
func (x *App) VerifyLLM(keyID string) (core.LLMCapabilities, error) { return x.a.VerifyLLM(keyID) }

func (x *App) SetRepoRoot(dir string) error { return x.a.SetRepoRoot(dir) }
func (x *App) ChooseRepoRoot() (string, error) {
	dir, e := runtime.OpenDirectoryDialog(x.ctx, runtime.OpenDialogOptions{Title: "选择 ImageWorkbench 源码目录"})
	if e != nil || dir == "" {
		return "", e
	}
	return dir, x.a.SetRepoRoot(dir)
}

var versionField = regexp.MustCompile(`"productVersion"\s*:\s*"([0-9.]+)"`)

// CurrentVersion is the app version in the source folder (wails.json).
func (x *App) CurrentVersion() string {
	b, e := os.ReadFile(filepath.Join(x.a.State().RepoRoot, "wails.json"))
	if e != nil {
		return ""
	}
	if m := versionField.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// BuildPackage starts a customer package build in the background; progress and
// the result arrive as "build:log" and "build:done" events.
func (x *App) BuildPackage(customerID, version string) error {
	if !x.building.TryLock() {
		return errors.New("正在生成另一个交付包，请等它完成")
	}
	go func() {
		defer x.building.Unlock()
		log := func(s string) { runtime.EventsEmit(x.ctx, "build:log", s) }
		d, e := x.a.BuildPackage(context.Background(), customerID, version, x.builder, admin.LocalDataDir(x.a.State().RepoRoot), log)
		result := map[string]any{"ok": e == nil}
		if e != nil {
			result["error"] = e.Error()
		} else {
			result["delivery"] = d
		}
		runtime.EventsEmit(x.ctx, "build:done", result)
	}()
	return nil
}

func (x *App) ExportSigningBackup(password string) (bool, error) {
	path, e := runtime.SaveFileDialog(x.ctx, runtime.SaveDialogOptions{Title: "保存签名密钥备份", DefaultFilename: "ImageWorkbench-签名密钥备份.json", Filters: []runtime.FileFilter{{DisplayName: "密钥备份", Pattern: "*.json"}}})
	if e != nil || path == "" {
		return false, e
	}
	return true, x.a.ExportSigningBackup(path, password)
}

func (x *App) ImportSigningBackup(password string, replace bool) (bool, error) {
	path, e := runtime.OpenFileDialog(x.ctx, runtime.OpenDialogOptions{Title: "选择签名密钥备份", Filters: []runtime.FileFilter{{DisplayName: "密钥备份", Pattern: "*.json"}}})
	if e != nil || path == "" {
		return false, e
	}
	return true, x.a.ImportSigningBackup(path, password, replace)
}

// OpenConsole opens an upstream's web backend (to set quotas by hand).
func (x *App) OpenConsole(upstreamID string) error {
	for _, u := range x.a.State().Upstreams {
		if u.ID == upstreamID && u.ConsoleURL != "" {
			runtime.BrowserOpenURL(x.ctx, u.ConsoleURL)
			return nil
		}
	}
	return errors.New("这个上游没有填写后台地址")
}

// ShowInFolder opens Explorer with the delivery ZIP selected.
func (x *App) ShowInFolder(path string) error {
	if _, e := os.Stat(path); e != nil {
		return errors.New("文件不存在，可能已被移动")
	}
	return exec.Command("explorer.exe", "/select,", path).Start()
}
