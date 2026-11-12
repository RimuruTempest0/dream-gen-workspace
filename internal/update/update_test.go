package update

import (
	"archive/zip"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func manifest() Manifest {
	return Manifest{Version: "0.7.13", MinVersion: "0.7.10", Notes: "修复", URL: "https://example.test/u.zip", SHA256: strings.Repeat("a", 64), Size: 10}
}

func TestManifestSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys := []string{base64.StdEncoding.EncodeToString(pub)}
	data, e := Sign(manifest(), priv)
	if e != nil {
		t.Fatal(e)
	}
	m, e := Verify(data, keys)
	if e != nil || m.Version != "0.7.13" || m.PublishedAt == "" {
		t.Fatal(m, e)
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if _, e = Verify(data, []string{base64.StdEncoding.EncodeToString(other)}); e != ErrSignature {
		t.Fatal("foreign key accepted")
	}
	tampered := strings.Replace(string(data), `"payload": "`, `"payload": "e`, 1)
	if _, e = Verify([]byte(tampered), keys); e != ErrSignature {
		t.Fatal("tampered manifest accepted")
	}
	bad := manifest()
	bad.Version = "0.7"
	if _, e = Sign(bad, priv); e == nil {
		t.Fatal("bad version signed")
	}
	bad = manifest()
	bad.URL = "ftp://x"
	if _, e = Sign(bad, priv); e == nil {
		t.Fatal("bad URL signed")
	}
}

func TestVersionRules(t *testing.T) {
	if Compare("0.7.10", "0.7.9") != 1 || Compare("0.7.9", "0.7.10") != -1 || Compare("1.0.0", "1.0.0") != 0 {
		t.Fatal("compare")
	}
	m := manifest()
	if ok, _ := m.Applies("0.7.13"); ok {
		t.Fatal("same version offered")
	}
	if ok, why := m.Applies("0.7.9"); ok || !strings.Contains(why, "0.7.10") {
		t.Fatal("below min version offered", why)
	}
	if ok, _ := m.Applies("0.7.12"); !ok {
		t.Fatal("update not offered")
	}
}

func TestAllowedEntries(t *testing.T) {
	for _, ok := range []string{"ImageWorkbench.exe", "product-browser.exe", "使用说明.txt", "version.json", "runtime/msedgewebview2.exe", "agent/claude-code/claude.exe"} {
		if !AllowedEntry(ok) {
			t.Fatal("refused", ok)
		}
	}
	for _, bad := range []string{"customer.dat", "data-location.txt", "data/app.db", "../x.exe", "runtime/../../x", "C:/Windows/x", "/abs", "other.exe", "runtime"} {
		if AllowedEntry(bad) {
			t.Fatal("allowed", bad)
		}
	}
}

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "u.zip")
	f, _ := os.Create(path)
	w := zip.NewWriter(f)
	for name, body := range files {
		x, _ := w.Create(name)
		x.Write([]byte(body))
	}
	w.Close()
	f.Close()
	return path
}

func TestExtractRefusesForeignFiles(t *testing.T) {
	good := writeZip(t, map[string]string{"ImageWorkbench.exe": "new", "version.json": `{"version":"0.7.13"}`})
	if e := Extract(good, filepath.Join(t.TempDir(), "s")); e != nil {
		t.Fatal(e)
	}
	for _, extra := range []string{"customer.dat", "data/app.db", "../evil.exe"} {
		pkg := writeZip(t, map[string]string{"ImageWorkbench.exe": "new", "version.json": "{}", extra: "x"})
		if e := Extract(pkg, filepath.Join(t.TempDir(), "s")); e == nil {
			t.Fatal("extracted", extra)
		}
	}
	if e := Extract(writeZip(t, map[string]string{"version.json": "{}"}), filepath.Join(t.TempDir(), "s")); e == nil {
		t.Fatal("package without program accepted")
	}
}

func TestComponentsMustMatchWithoutRuntime(t *testing.T) {
	program, stage := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(program, "runtime"), 0o700)
	os.WriteFile(filepath.Join(program, "runtime", "msedgewebview2.exe"), []byte("runtime-1"), 0o600)
	have, _ := FileSHA256(filepath.Join(program, "runtime", "msedgewebview2.exe"))
	os.WriteFile(filepath.Join(stage, "version.json"), []byte(`{"version":"0.7.13","runtimeSHA256":"`+strings.ToUpper(have)+`"}`), 0o600)
	if e := CheckComponents(stage, program); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(stage, "version.json"), []byte(`{"version":"0.7.13","runtimeSHA256":"`+strings.Repeat("b", 64)+`"}`), 0o600)
	if e := CheckComponents(stage, program); e == nil || !strings.Contains(e.Error(), "完整包") {
		t.Fatal("new runtime not required", e)
	}
	// A package that brings its runtime needs no match.
	os.MkdirAll(filepath.Join(stage, "runtime"), 0o700)
	if e := CheckComponents(stage, program); e != nil {
		t.Fatal(e)
	}
}

func layout(t *testing.T) (program, stage string) {
	program, stage = t.TempDir(), t.TempDir()
	for name, body := range map[string]string{"ImageWorkbench.exe": "old", "product-browser.exe": "old-browser", "customer.dat": "licence", "data-location.txt": "D:/data", "data/app.db": "db"} {
		os.MkdirAll(filepath.Dir(filepath.Join(program, name)), 0o700)
		os.WriteFile(filepath.Join(program, name), []byte(body), 0o600)
	}
	os.WriteFile(filepath.Join(stage, "ImageWorkbench.exe"), []byte("new"), 0o600)
	os.WriteFile(filepath.Join(stage, "product-browser.exe"), []byte("new-browser"), 0o600)
	os.WriteFile(filepath.Join(stage, "version.json"), []byte("{}"), 0o600)
	return
}

func read(path string) string { b, _ := os.ReadFile(path); return string(b) }

func TestApplyReplacesProgramOnly(t *testing.T) {
	program, stage := layout(t)
	if e := Apply(stage, program, filepath.Join(program, ".update", "backup")); e != nil {
		t.Fatal(e)
	}
	if read(filepath.Join(program, "ImageWorkbench.exe")) != "new" || read(filepath.Join(program, "product-browser.exe")) != "new-browser" || read(filepath.Join(program, "version.json")) != "{}" {
		t.Fatal("program not replaced")
	}
	if read(filepath.Join(program, "customer.dat")) != "licence" || read(filepath.Join(program, "data-location.txt")) != "D:/data" || read(filepath.Join(program, "data", "app.db")) != "db" {
		t.Fatal("data touched")
	}
	if read(filepath.Join(program, ".update", "backup", "ImageWorkbench.exe")) != "old" {
		t.Fatal("no backup")
	}
}

func TestApplyRollsBackWhenAFileIsLocked(t *testing.T) {
	program, stage := layout(t)
	old := RenameWait
	RenameWait = 200 * time.Millisecond
	defer func() { RenameWait = old }()
	// The running browser helper keeps its file open, so it cannot be moved aside.
	lock, _ := os.Open(filepath.Join(program, "product-browser.exe"))
	defer lock.Close()
	e := Apply(stage, program, filepath.Join(program, ".update", "backup"))
	if e == nil {
		if runtime.GOOS == "windows" {
			t.Fatal("a locked program file did not stop the update")
		}
		t.Skip("the platform allowed the move; rollback not exercised")
	}
	if read(filepath.Join(program, "ImageWorkbench.exe")) != "old" || read(filepath.Join(program, "product-browser.exe")) != "old-browser" {
		t.Fatal("not rolled back:", e)
	}
}
