package core

import (
	"crypto/sha256"
	"encoding/hex"
	"imageworkbench/internal/i18n"
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
func verifyBundled(path, expected string, label i18n.Message) error {
	if expected == "" {
		return nil
	}
	info, e := os.Stat(path)
	if e != nil {
		return i18n.Fail("data.bundled.missing", "name", label)
	}
	bundledHashes.Lock()
	cached, ok := bundledHashes.m[path]
	bundledHashes.Unlock()
	if !ok || cached.size != info.Size() || !cached.mod.Equal(info.ModTime()) {
		f, e := os.Open(path)
		if e != nil {
			return i18n.Fail("data.bundled.unreadable", "name", label, "error", e)
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return i18n.Fail("data.bundled.unreadable", "name", label, "error", e)
		}
		cached = bundledHash{size: info.Size(), mod: info.ModTime(), hash: hex.EncodeToString(h.Sum(nil))}
		bundledHashes.Lock()
		bundledHashes.m[path] = cached
		bundledHashes.Unlock()
	}
	if !strings.EqualFold(cached.hash, expected) {
		return i18n.Fail("data.bundled.changed", "name", label)
	}
	return nil
}
