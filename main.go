package main

import (
	"context"
	"embed"
	"fmt"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"imageworkbench/internal/core"
	"os"
	"path/filepath"
	"strings"
)

// productBrowserExe is the helper built from cmd/product-browser and shipped next to the app.
const productBrowserExe = "product-browser.exe"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if bindingsMode {
		if e := wails.Run(&options.App{Bind: []interface{}{&App{}}}); e != nil {
			fmt.Fprintln(os.Stderr, e)
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
		fatal("便携包缺少 runtime 目录，请完整解压应用文件。开发模式可设置 IMAGEWORKBENCH_ALLOW_SYSTEM_RUNTIME=1。")
		return
	}
	if e := prepareRuntime(runtimePath); e != nil {
		fatal("运行时目录权限检查失败：" + e.Error())
		return
	}
	s, e := core.NewService(data)
	needDirectory := e != nil
	cache := filepath.Join(root, "cache", "webview")
	if needDirectory {
		fallback, err := os.MkdirTemp("", "ImageWorkbench-bootstrap-")
		if err != nil {
			fatal("无法创建临时目录，请将应用移到可写目录。")
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
	app := &App{service: s, configRoot: root}
	s.SetAgentExecutable(filepath.Join(root, "agent", "claude-code", "claude.exe"))
	s.SetProductBrowser(filepath.Join(root, productBrowserExe), runtimePath)
	// A customer package carries customer.dat beside the program; none = developer mode.
	s.LoadLicense(root)
	e = wails.Run(&options.App{Title: "生图工作台", Width: 1440, Height: 920, MinWidth: 800, MinHeight: 600, BackgroundColour: &options.RGBA{R: 245, G: 246, B: 248, A: 1}, AssetServer: &assetserver.Options{Assets: assets},
		OnStartup: func(ctx context.Context) {
			if needDirectory {
				path, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: "应用目录不可写，请选择本地数据目录"})
				if err != nil || path == "" {
					fatal("未选择可写的数据目录，应用将退出。")
					os.Exit(1)
				}
				replacement, err := core.NewService(path)
				if err != nil {
					fatal("数据目录无法使用：" + err.Error())
					os.Exit(1)
				}
				app.service.Dispose()
				app.service = replacement
				replacement.SetAgentExecutable(filepath.Join(root, "agent", "claude-code", "claude.exe"))
				replacement.SetProductBrowser(filepath.Join(root, productBrowserExe), runtimePath)
				replacement.LoadLicense(root)
				saveDataPath(root, path)
			}
			app.startup(ctx)
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
		OnShutdown: app.shutdown, OnBeforeClose: app.beforeClose, Bind: []interface{}{app}, DragAndDrop: &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true}, Windows: &windows.Options{WebviewBrowserPath: runtimePath, WebviewUserDataPath: cache, Theme: windows.Light}})
	if e != nil {
		fatal(fmt.Sprintf("应用启动失败：%v", e))
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
		return fmt.Errorf("应用设置目录不可用")
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
