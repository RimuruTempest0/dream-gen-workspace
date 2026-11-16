//go:build desktopqa

package core

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"imageworkbench/internal/update"
)

// PrepareTestUpdate makes this QA process offer version 9.9.9 from base (a local
// mock server): it trusts a fresh signing key and returns the signed update.json
// and the update package for the mock to serve. base "" switches updates off and
// forgets the key again.
func (s *Service) PrepareTestUpdate(base string) (manifest, pkg []byte, e error) {
	if base == "" {
		updateManifestURL, licensePublicKey = "", ""
		startUpdater = realStartUpdater
		s.mu.Lock()
		s.upd = updateState{}
		s.mu.Unlock()
		return nil, nil, nil
	}
	pub, priv, e := ed25519.GenerateKey(nil)
	if e != nil {
		return nil, nil, e
	}
	licensePublicKey = base64.StdEncoding.EncodeToString(pub)
	// The QA package is not a program: the one-click update stops at the restart.
	startUpdater = func(string, ...string) error { return errors.New("测试环境不安装更新") }
	updateManifestURL = strings.TrimSuffix(base, "/") + "/update.json"
	s.mu.Lock()
	program := s.programDir
	s.upd = updateState{}
	s.mu.Unlock()
	runtimeHash, _ := update.FileSHA256(filepath.Join(program, "runtime", "msedgewebview2.exe"))
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range map[string]string{
		"ImageWorkbench.exe": "QA update program (never installed by the tests)",
		"version.json":       `{"version":"9.9.9","runtimeSHA256":"` + strings.ToUpper(runtimeHash) + `"}`,
		"使用说明.txt":           "QA",
	} {
		f, e := w.Create(name)
		if e != nil {
			return nil, nil, e
		}
		f.Write([]byte(body))
	}
	if e = w.Close(); e != nil {
		return nil, nil, e
	}
	sum := sha256.Sum256(b.Bytes())
	manifest, e = update.Sign(update.Manifest{Version: "9.9.9", Notes: "QA 测试更新：只验证检查、下载和校验。", URL: strings.TrimSuffix(base, "/") + "/ImageWorkbench-update-9.9.9.zip", SHA256: hex.EncodeToString(sum[:]), Size: int64(b.Len())}, priv)
	_ = os.MkdirAll(filepath.Join(program, updateDir), 0o700)
	return manifest, b.Bytes(), e
}
