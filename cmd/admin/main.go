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

// CreateCustomer creates a person on a product line: "ecommerce" (a customer) or
// "creative" (a hosted user of the mass-market line).
func (x *App) CreateCustomer(name string, trial int, line string) (admin.Customer, error) {
	return x.a.CreateCustomerOn(line, name, trial)
}

// HostedSource is the built mass-market version a hosted package would be made of.
func (x *App) HostedSource() admin.HostedSource { return x.a.HostedSource() }
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

// LineSource is the commit and version a build of a product line ("ecommerce"
// or "creative") would take now, for the two build pages.
func (x *App) LineSource(line string) admin.LineSource { return x.a.LineSource(line) }

// BuildPackage starts a customer package build in the background; progress and
// the result arrive as "build:log" and "build:done" events.
func (x *App) BuildPackage(customerID, version string) error {
	if !x.building.TryLock() {
		return errors.New("正在进行另一次构建，请等它完成")
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

// CreativeNotes proposes the update notes for a version from docs/CHANGELOG.md.
func (x *App) CreativeNotes(version string) string { return x.a.CreativeNotes(version) }

// BuildCreative builds, checks and signs a version of the mass-market line in
// the background; progress and the result arrive as "creative:log" and
// "creative:done". Nothing is uploaded.
func (x *App) BuildCreative(version, notes string) error {
	if !x.building.TryLock() {
		return errors.New("正在进行另一次构建，请等它完成")
	}
	go func() {
		defer x.building.Unlock()
		log := func(s string) { runtime.EventsEmit(x.ctx, "creative:log", s) }
		r, e := x.a.BuildCreative(context.Background(), version, notes, x.builder, admin.LocalDataDir(x.a.State().RepoRoot), log)
		result := map[string]any{"ok": e == nil}
		if e != nil {
			result["error"] = e.Error()
		} else {
			result["release"] = r
		}
		runtime.EventsEmit(x.ctx, "creative:done", result)
	}()
	return nil
}

// OpenCreativeRelease opens the fixed GitHub pre-release the two files go to.
func (x *App) OpenCreativeRelease() {
	runtime.BrowserOpenURL(x.ctx, admin.CreativeReleasePage)
}

func (x *App) SetCustomerSkills(id string, ids []string) error {
	return x.a.SetCustomerSkills(id, ids)
}

func (x *App) SetCustomerExpiry(id, expiresAt string) error {
	return x.a.SetCustomerExpiry(id, expiresAt)
}

// SaveCustomerFile writes a renewal customer.dat (new expiry or keys) where the
// provider chooses; only this file needs to reach the customer.
func (x *App) SaveCustomerFile(customerID string) (bool, error) {
	path, e := runtime.SaveFileDialog(x.ctx, runtime.SaveDialogOptions{Title: "保存续费授权文件", DefaultFilename: "customer.dat", Filters: []runtime.FileFilter{{DisplayName: "授权文件", Pattern: "*.dat"}}})
	if e != nil || path == "" {
		return false, e
	}
	return true, x.a.WriteCustomerFile(customerID, path)
}

func (x *App) SetUpdateURL(url string) error { return x.a.SetUpdateURL(url) }

// ChooseUpdatePackage picks a program update package (ImageWorkbench-update-….zip).
func (x *App) ChooseUpdatePackage() (string, error) {
	return runtime.OpenFileDialog(x.ctx, runtime.OpenDialogOptions{Title: "选择程序更新包", Filters: []runtime.FileFilter{{DisplayName: "程序更新包", Pattern: "*.zip"}}})
}

// PublishUpdate signs update.json for the package and saves it where the
// provider chooses; both files are then uploaded by the provider.
func (x *App) PublishUpdate(r admin.UpdateRelease) (bool, error) {
	path, e := runtime.SaveFileDialog(x.ctx, runtime.SaveDialogOptions{Title: "保存更新清单", DefaultFilename: "update.json", Filters: []runtime.FileFilter{{DisplayName: "更新清单", Pattern: "*.json"}}})
	if e != nil || path == "" {
		return false, e
	}
	_, e = x.a.PublishUpdate(r, path)
	return e == nil, e
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
