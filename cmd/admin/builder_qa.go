//go:build desktopqa

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"

	"imageworkbench/internal/admin"
)

// qaBuilder stands in for scripts/build.ps1 in desktop QA: it writes small
// program files so packaging, checking and the ledger run for real in seconds.
// For a product-line build it also writes the program update package, as the
// real build does next to its own ZIP.
type qaBuilder struct{}

func (qaBuilder) Build(_ context.Context, b admin.BuildRequest, log func(string)) error {
	dir := filepath.Join(b.RepoRoot, b.ReleaseDir)
	version := `{"version":"` + b.Version + `","productLine":"` + b.ProductLine + `"}`
	for name, body := range map[string]string{"ImageWorkbench.exe": "qa", "product-browser.exe": "qa", "runtime/msedgewebview2.exe": "qa", "agent/claude-code/claude.exe": "qa", "使用说明.txt": "qa", "version.json": version} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if e := os.WriteFile(p, []byte(body), 0o644); e != nil {
			return e
		}
	}
	if b.ProductLine != "" {
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		for name, body := range map[string]string{"ImageWorkbench.exe": "qa", "version.json": version} {
			f, e := w.Create(name)
			if e != nil {
				return e
			}
			f.Write([]byte(body))
		}
		if e := w.Close(); e != nil {
			return e
		}
		if e := os.WriteFile(filepath.Join(b.RepoRoot, strings.TrimSuffix(b.ZipPath, ".zip")+"-update.zip"), buf.Bytes(), 0o644); e != nil {
			return e
		}
	}
	log("QA 构建：已写入测试用程序文件")
	return nil
}

func builder() admin.Builder { return qaBuilder{} }
