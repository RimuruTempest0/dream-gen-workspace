package update

import (
	"archive/zip"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"imageworkbench/internal/i18n"
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
	m, e := Verify(data, keys, "")
	if e != nil || m.Version != "0.7.13" || m.PublishedAt == "" {
		t.Fatal(m, e)
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if _, e = Verify(data, []string{base64.StdEncoding.EncodeToString(other)}, ""); e != ErrSignature {
		t.Fatal("foreign key accepted")
	}
	tampered := strings.Replace(string(data), `"payload": "`, `"payload": "e`, 1)
	if _, e = Verify([]byte(tampered), keys, ""); e != ErrSignature {
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
	if ok, why := m.Applies("0.7.9"); ok || why == nil || !strings.Contains(why.Error(), "0.7.10") {
		t.Fatal("below min version offered", why)
	}
	if ok, _ := m.Applies("0.7.12"); !ok {
		t.Fatal("update not offered")
	}
}

func TestAllowedEntries(t *testing.T) {
	for _, ok := range []string{"ImageWorkbench.exe", "product-browser.exe", "使用说明.txt", "User Guide.txt", "Hướng dẫn sử dụng.txt", "使い方ガイド.txt", "사용 설명서.txt", "version.json", "runtime/msedgewebview2.exe", "agent/claude-code/claude.exe"} {
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

// build.ps1 packs with PowerShell, which writes backslash paths and names each
// folder with a trailing backslash. Such a folder entry must not become a file
// that then blocks the folder (packages that carry the browser runtime have them).
func TestExtractPowerShellFolderEntries(t *testing.T) {
	pkg := filepath.Join(t.TempDir(), "u.zip")
	f, _ := os.Create(pkg)
	w := zip.NewWriter(f)
	for _, entry := range []struct{ name, body string }{
		{"ImageWorkbench.exe", "new"},
		{"version.json", `{"version":"0.7.32"}`},
		{`runtime\EBWebView\`, ""},
		{`runtime\EBWebView\x64\EmbeddedBrowserWebView.dll`, "dll"},
		{`runtime\Dictionaries\`, ""},
	} {
		x, _ := w.Create(entry.name)
		x.Write([]byte(entry.body))
	}
	w.Close()
	f.Close()
	stage := filepath.Join(t.TempDir(), "s")
	if e := Extract(pkg, stage); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(filepath.Join(stage, "runtime", "EBWebView", "x64", "EmbeddedBrowserWebView.dll")); e != nil || string(b) != "dll" {
		t.Fatal("file below a folder entry not extracted", e)
	}
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

// A manifest of one product line must never be accepted by the other: delivered
// e-commerce apps read format 1 only, and a product-line app reads format 2 for
// its own line only, even though both are signed with the same key.
func TestProductLinesDoNotAcceptEachOther(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys := []string{base64.StdEncoding.EncodeToString(pub)}
	m := Manifest{Version: "0.7.32", URL: "https://example.com/u.zip", SHA256: strings.Repeat("a", 64), Size: 10}
	plain, e := Sign(m, priv)
	if e != nil {
		t.Fatal(e)
	}
	m.Line = "creative"
	creative, e := Sign(m, priv)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(plain), `"format": 1`) || !strings.Contains(string(creative), `"format": 2`) {
		t.Fatal("formats", string(plain), string(creative))
	}
	if got, e := Verify(creative, keys, "creative"); e != nil || got.Line != "creative" || got.Version != "0.7.32" {
		t.Fatal("own line refused", got, e)
	}
	if _, e := Verify(creative, keys, ""); e == nil {
		t.Fatal("e-commerce app accepted a creative manifest")
	}
	if _, e := Verify(plain, keys, "creative"); e == nil {
		t.Fatal("creative app accepted an e-commerce manifest")
	}
	if _, e := Verify(creative, keys, "other"); e == nil {
		t.Fatal("another line accepted a creative manifest")
	}
	// Relabelling the outer format does not help: the signed payload names it too.
	if _, e := Verify([]byte(strings.Replace(string(creative), `"format": 2`, `"format": 1`, 1)), keys, ""); e == nil {
		t.Fatal("relabelled manifest accepted")
	}
	m.Line = "Creative Line"
	if _, e := Sign(m, priv); e == nil {
		t.Fatal("malformed line name signed")
	}
}

// Every 《使用说明》 the package ships exists in docs/, matches the Chinese one
// line for line, and the non-Chinese ones name the software Image Workbench.
func TestManualsExistInEveryLanguage(t *testing.T) {
	lines := func(name string) []string {
		b, e := os.ReadFile(filepath.Join("..", "..", "docs", name))
		if e != nil {
			t.Fatal(e)
		}
		return strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	}
	zh := lines(Manuals[0])
	for _, name := range Manuals[1:] {
		got := lines(name)
		if len(got) != len(zh) {
			t.Errorf("%s has %d lines, the Chinese manual %d", name, len(got), len(zh))
		}
		for i := range min(len(got), len(zh)) {
			if (got[i] == "") != (zh[i] == "") {
				t.Errorf("%s line %d: paragraph breaks differ from the Chinese manual", name, i+1)
			}
		}
		text := strings.Join(got, "\n")
		if !strings.HasPrefix(text, "Image Workbench") || strings.Contains(text, "生图工作台") {
			t.Errorf("%s must name the software Image Workbench", name)
		}
	}
}

// Each manual line that uses a fixed term in Chinese uses the glossary's
// translation on the same line of the other manuals, so the guides call
// things what the interface calls them.
func TestManualsUseTheGlossary(t *testing.T) {
	read := func(name string) []string {
		b, e := os.ReadFile(filepath.Join("..", "..", "docs", name))
		if e != nil {
			t.Fatal(e)
		}
		return strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	}
	zh := read(Manuals[0])
	for i, lang := range []string{"en", "vi", "ja", "ko"} {
		name := Manuals[i+1]
		lines := read(name)
		for n, line := range zh {
			if n >= len(lines) {
				break
			}
			for _, term := range i18n.Glossary() {
				if strings.Contains(line, term["zh-CN"]) && !strings.Contains(strings.ToLower(lines[n]), strings.ToLower(term[lang])) {
					t.Errorf("%s line %d: %s must be %q", name, n+1, term["zh-CN"], term[lang])
				}
			}
		}
	}
}

// Chinese interface: the Chinese notes. English, Vietnamese, Japanese and
// Korean: the English notes, or the Chinese ones when there are none.
func TestNotesFollowTheInterfaceLanguage(t *testing.T) {
	m := manifest()
	m.NotesEn = "Fixes"
	if n, l := m.NotesFor("zh-CN"); n != "修复" || l != "zh-CN" {
		t.Fatal("zh-CN", n, l)
	}
	for _, lang := range []string{"en", "vi", "ja", "ko"} {
		if n, l := m.NotesFor(lang); n != "Fixes" || l != "en" {
			t.Fatal(lang, n, l)
		}
	}
	m.NotesEn = "  "
	for _, lang := range []string{"zh-CN", "en", "vi", "ja", "ko"} {
		if n, l := m.NotesFor(lang); n != "修复" || l != "zh-CN" {
			t.Fatal("fallback", lang, n, l)
		}
	}
}

// A manifest signed before 0.8.0 (no English notes) is still read, and an app
// before 0.8.0 reading a new manifest finds its Chinese notes where it always did.
func TestManifestNotesStayCompatible(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys := []string{base64.StdEncoding.EncodeToString(pub)}

	// Old format: a payload exactly as 0.7.x manifests carry it.
	payload := []byte(`{"format":1,"version":"0.7.13","minVersion":"0.7.10","publishedAt":"2026-01-01T00:00:00Z","notes":"修复","url":"https://example.test/u.zip","sha256":"` + strings.Repeat("a", 64) + `","size":10}`)
	old, _ := json.Marshal(signedFile{Format: 1, Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))})
	m, e := Verify(old, keys, "")
	if e != nil || m.NotesEn != "" {
		t.Fatal(m, e)
	}
	if n, _ := m.NotesFor("en"); n != "修复" {
		t.Fatal("old manifest in English", n)
	}

	// New format, read as an app before 0.8.0 does: its manifest type has no
	// English field, and the signature covers the payload bytes as they are.
	nm := manifest()
	nm.NotesEn = "Fixes"
	data, e := Sign(nm, priv)
	if e != nil {
		t.Fatal(e)
	}
	var f signedFile
	json.Unmarshal(data, &f)
	raw, _ := base64.StdEncoding.DecodeString(f.Payload)
	sig, _ := base64.StdEncoding.DecodeString(f.Signature)
	if !ed25519.Verify(pub, raw, sig) {
		t.Fatal("signature")
	}
	var before struct {
		Format  int    `json:"format"`
		Version string `json:"version"`
		Notes   string `json:"notes"`
		URL     string `json:"url"`
	}
	if e := json.Unmarshal(raw, &before); e != nil || before.Notes != "修复" || before.Version != "0.7.13" || before.Format != 1 {
		t.Fatal("old app reads", before, e)
	}
	if !strings.Contains(string(raw), `"notesEn":"Fixes"`) {
		t.Fatal("English notes not in the payload", string(raw))
	}
}
