//go:build desktopqa

package main

import (
	"context"
	"os"
	"path/filepath"

	"imageworkbench/internal/admin"
)

// qaBuilder stands in for scripts/build.ps1 in desktop QA: it writes small
// program files so packaging, checking and the ledger run for real in seconds.
type qaBuilder struct{}

func (qaBuilder) Build(_ context.Context, b admin.BuildRequest, log func(string)) error {
	dir := filepath.Join(b.RepoRoot, b.ReleaseDir)
	for name, body := range map[string]string{"ImageWorkbench.exe": "qa", "product-browser.exe": "qa", "runtime/msedgewebview2.exe": "qa", "agent/claude-code/claude.exe": "qa", "使用说明.txt": "qa", "version.json": "{}"} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if e := os.WriteFile(p, []byte(body), 0o644); e != nil {
			return e
		}
	}
	log("QA 构建：已写入测试用程序文件")
	return nil
}

func builder() admin.Builder { return qaBuilder{} }
