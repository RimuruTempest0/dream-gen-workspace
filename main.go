package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"imageworkbench/internal/core"
	"imageworkbench/internal/i18n"
	"os"
	"path/filepath"
	"strings"
)

// productBrowserExe is the helper built from cmd/product-browser and shipped next to the app.
const productBrowserExe = "product-browser.exe"

//go:embed all:frontend/dist
var assets embed.FS

// bootLang is the interface language of messages shown before the data
// directory is open (the Windows UI language, as on a first start); it follows
// the saved preference once a service exists.
var bootLang = i18n.FromSystem(i18n.SystemLanguages()...)

func bootMsg(key string, args ...any) string { return i18n.T(bootLang, key, args...) }

func main() {
	if bindingsMode {
		if e := wails.Run(&options.App{Bind: []interface{}{&App{}}}); e != nil {
			fmt.Fprintln(os.Stderr, e)
		}
		return
	}
	// The downloaded new version applies itself: it waits for the old program to
	// exit, replaces the program files and starts it again.
	if core.IsUpdateCommand(os.Args) {
		if e := core.RunUpdateCommand(os.Args, core.WaitForExit); e != nil {
			bootLang = core.UpdateCommandLanguage(os.Args)
			fatal(bootMsg("update.command.fatal", "error", e))
		}
		return
	}
	exe, e := os.Executable()
	release, primary, lockErr := claimInstance()
	if lockErr != nil {
		fatal(lockErr.Error())
		return
	}
	if !primary {
		return
	}
	defer release()
	if e != nil {
		fatal(e.Error())
		return
	}
	root := filepath.Dir(exe)
	core.CleanupUpdates(root)
	data := filepath.Join(root, "data")
	if path := savedDataPath(root); path != "" {
		data = path
	}
	if path := os.Getenv("IMAGEWORKBENCH_DATA_DIR"); path != "" {
		data, e = filepath.Abs(path)
		if e != nil {
			fatal(e.Error())
			return
		}
	}
	launch(root, data)
}
func launch(root, data string) {
	runtimePath := filepath.Join(root, "runtime")
	if os.Getenv("IMAGEWORKBENCH_ALLOW_SYSTEM_RUNTIME") == "1" {
		runtimePath = ""
	} else if _, e := os.Stat(filepath.Join(runtimePath, "msedgewebview2.exe")); e != nil {
		fatal(bootMsg("app.boot.noRuntime"))
		return
	}
	if e := prepareRuntime(runtimePath); e != nil {
		fatal(bootMsg("app.boot.runtimeAccess", "error", e))
		return
	}
	s, e := core.NewService(data)
	needDirectory := e != nil
	cache := filepath.Join(root, "cache", "webview")
	if needDirectory {
		fallback, err := os.MkdirTemp("", "ImageWorkbench-bootstrap-")
		if err != nil {
			fatal(bootMsg("app.boot.noTemp"))
			return
		}
		defer os.RemoveAll(fallback)
		s, e = core.NewService(filepath.Join(fallback, "data"))
		cache = filepath.Join(fallback, "cache")
		if e != nil {
			fatal(e.Error())
			return
		}
	}
	bootLang = s.Language()
	app := &App{service: s, configRoot: root}
	s.SetAgentExecutable(filepath.Join(root, "agent", "claude-code", "claude.exe"))
	s.SetProductBrowser(filepath.Join(root, productBrowserExe), runtimePath)
	// A customer package carries customer.dat beside the program; none = developer mode.
	s.LoadLicense(root)
	e = wails.Run(&options.App{Title: i18n.AppName(s.Language()), Width: 1440, Height: 920, MinWidth: 800, MinHeight: 600, BackgroundColour: windowBackground(s.Appearance()), AssetServer: &assetserver.Options{Assets: assets},
		OnStartup: func(ctx context.Context) {
			if needDirectory {
				path, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: bootMsg("app.boot.chooseData")})
				if err != nil || path == "" {
					fatal(bootMsg("app.boot.noData"))
					os.Exit(1)
				}
				replacement, err := core.NewService(path)
				if err != nil {
					fatal(bootMsg("app.boot.dataUnusable", "error", err))
					os.Exit(1)
				}
				bootLang = replacement.Language()
				app.service.Dispose()
				app.service = replacement
				replacement.SetAgentExecutable(filepath.Join(root, "agent", "claude-code", "claude.exe"))
				replacement.SetProductBrowser(filepath.Join(root, productBrowserExe), runtimePath)
				replacement.LoadLicense(root)
				saveDataPath(root, path)
				applyWindowAppearance(ctx, replacement.Appearance())
			}
			app.startup(ctx)
			app.watchAppearance(ctx)
			runtime.OnFileDrop(ctx, func(x, y int, paths []string) {
				items, err := app.importPaths(paths)
				if err != nil {
					runtime.EventsEmit(ctx, "files:error", err.Error())
				}
				if len(items) > 0 {
					runtime.EventsEmit(ctx, "files:imported", items)
				}
			})
		},
		OnShutdown: app.shutdown, OnBeforeClose: app.beforeClose, Bind: []interface{}{app}, DragAndDrop: &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true}, Windows: &windows.Options{WebviewBrowserPath: runtimePath, WebviewUserDataPath: cache, Theme: windowTheme(s.Appearance()), CustomTheme: titleBarTheme()}})
	if e != nil {
		fatal(bootMsg("app.boot.failed", "error", e))
	}
}
func savedDataPath(root string) string {
	b, e := os.ReadFile(filepath.Join(root, "data-location.txt"))
	if e != nil {
		return ""
	}
	path := strings.TrimSpace(string(b))
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return filepath.Clean(path)
}
func saveDataPath(root, path string) error {
	if root == "" {
		return errors.New(bootMsg("app.boot.noConfigDir"))
	}
	if rel, e := filepath.Rel(root, path); e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		path = rel
	}
	temp, e := os.CreateTemp(root, ".data-location-*")
	if e != nil {
		return e
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, e = temp.WriteString(path); e != nil {
		temp.Close()
		return e
	}
	if e = temp.Sync(); e != nil {
		temp.Close()
		return e
	}
	if e = temp.Close(); e != nil {
		return e
	}
	return os.Rename(name, filepath.Join(root, "data-location.txt"))
}
