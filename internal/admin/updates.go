package admin

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"imageworkbench/internal/update"
)

// packageVersion reads version.json from an update package and checks that the
// package holds only program files.
func packageVersion(path string) (string, error) {
	z, e := zip.OpenReader(path)
	if e != nil {
		return "", errors.New("请选择程序更新包（ImageWorkbench-update-….zip）")
	}
	defer z.Close()
	version := ""
	for _, f := range z.File {
		name := strings.ReplaceAll(f.Name, string(os.PathSeparator), "/")
		// PowerShell names folders with a trailing backslash.
		if f.FileInfo().IsDir() || strings.HasSuffix(name, "/") {
			continue
		}
		if !update.AllowedEntry(name) {
			return "", errors.New("这个 ZIP 不是程序更新包：包含 " + name)
		}
		if name == "version.json" {
			r, e := f.Open()
			if e != nil {
				return "", e
			}
			var v struct {
				Version string `json:"version"`
			}
			b, e := io.ReadAll(io.LimitReader(r, 1<<20))
			r.Close()
			if e != nil || json.Unmarshal(update.TrimBOM(b), &v) != nil {
				return "", errors.New("更新包的 version.json 无法读取")
			}
			version = v.Version
		}
	}
	if version == "" {
		return "", errors.New("更新包里没有 version.json")
	}
	return version, nil
}

func fileExists(path string) bool {
	_, e := os.Stat(path)
	return e == nil
}
