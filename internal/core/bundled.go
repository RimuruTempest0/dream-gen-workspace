package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// SHA256 of the shipped helper programs, set by scripts/build.ps1 with
// -ldflags "-X imageworkbench/internal/core.expectedClaudeSHA256=…". Development
// and test builds leave them empty and skip the check.
var (
	expectedClaudeSHA256         = ""
	expectedProductBrowserSHA256 = ""
)

type bundledHash struct {
	size int64
	mod  time.Time
	hash string
}

var bundledHashes = struct {
	sync.Mutex
	m map[string]bundledHash
}{m: map[string]bundledHash{}}

// verifyBundled refuses to start a shipped helper whose file differs from the one
// this build was packaged with. The hash is remembered until the file changes.
func verifyBundled(path, expected, label string) error {
	if expected == "" {
		return nil
	}
	info, e := os.Stat(path)
	if e != nil {
		return fmt.Errorf("%s缺失，请重新完整解压软件包", label)
	}
	bundledHashes.Lock()
	cached, ok := bundledHashes.m[path]
	bundledHashes.Unlock()
	if !ok || cached.size != info.Size() || !cached.mod.Equal(info.ModTime()) {
		f, e := os.Open(path)
		if e != nil {
			return fmt.Errorf("无法读取%s：%w", label, e)
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return fmt.Errorf("无法读取%s：%w", label, e)
		}
		cached = bundledHash{size: info.Size(), mod: info.ModTime(), hash: hex.EncodeToString(h.Sum(nil))}
		bundledHashes.Lock()
		bundledHashes.m[path] = cached
		bundledHashes.Unlock()
	}
	if !strings.EqualFold(cached.hash, expected) {
		return fmt.Errorf("%s与本版本不一致（文件可能损坏或被替换），已停止运行。请关闭软件，重新完整解压软件包后再试。", label)
	}
	return nil
}
