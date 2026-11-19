package core

import (
	"imageworkbench/internal/i18n"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBundledHelpersAreCheckedBeforeStarting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper.exe")
	os.WriteFile(path, []byte("shipped helper"), 0o600)
	sum := sha256.Sum256([]byte("shipped helper"))
	good := hex.EncodeToString(sum[:])
	if e := verifyBundled(path, "", i18n.Msg("update.component.claude")); e != nil {
		t.Fatal("development build checked", e)
	}
	if e := verifyBundled(path, strings.ToUpper(good), i18n.Msg("update.component.claude")); e != nil {
		t.Fatal("matching file refused", e)
	}
	// Replaced after the first check: the remembered hash must not hide it.
	os.WriteFile(path, []byte("replaced helper!"), 0o600)
	os.Chtimes(path, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	if e := verifyBundled(path, good, i18n.Msg("update.component.claude")); e == nil || !strings.Contains(e.Error(), "重新完整解压") {
		t.Fatal("replaced file accepted", e)
	}
	if e := verifyBundled(filepath.Join(t.TempDir(), "missing.exe"), good, i18n.Msg("update.component.claude")); e == nil {
		t.Fatal("missing file accepted")
	}

	old := expectedProductBrowserSHA256
	expectedProductBrowserSHA256 = good
	defer func() { expectedProductBrowserSHA256 = old }()
	if _, e := startProductBrowser(productBrowserConfig{Executable: path}, t.TempDir(), "https://example.com"); e == nil || !strings.Contains(e.Error(), "product-browser.exe") {
		t.Fatal("replaced browser helper started", e)
	}
}
