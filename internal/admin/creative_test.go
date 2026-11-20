package admin

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imageworkbench/internal/update"
)

func creativeRepo(t *testing.T, a *Admin, version string) string {
	t.Helper()
	dir := repo(t, a)
	a.changelog = func(string) ([]byte, error) {
		return []byte("# 更新记录\r\n\r\n## " + version + " · 一键更新\r\n\r\n大众版可以一键更新。\r\n\r\n- 点一次即可\r\n\r\n### 验证\r\n- 内部说明\r\n\r\n## 0.0.1 · 旧\r\n\r\n旧说明\r\n"), nil
	}
	sourceAt(a, version)
	return dir
}

func zipNames(t *testing.T, path string) []string {
	t.Helper()
	z, e := zip.OpenReader(path)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	return names
}

func TestCreativeReleaseIsBuiltSignedAndLaidOutForUpload(t *testing.T) {
	a := open(t)
	dir := creativeRepo(t, a, "0.7.32")
	if notes := a.CreativeNotes("0.7.32"); !strings.Contains(notes, "大众版可以一键更新") || !strings.Contains(notes, "点一次即可") || strings.Contains(notes, "内部说明") || strings.Contains(notes, "旧说明") {
		t.Fatalf("notes from the changelog: %q", notes)
	}
	var req BuildRequest
	r, e := a.BuildCreative(context.Background(), "0.7.32", "可以一键更新了", "One-click updates are here", fakeBuilder{update: true, seen: &req}, "", func(string) {})
	if e != nil {
		t.Fatal(e)
	}
	if req.ProductLine != "creative" || req.UpdateKey != a.State().SigningPublicKey || req.UpdateURL != CreativeReleaseURL+"update.json" {
		t.Fatalf("build request %+v", req)
	}
	// The full package stays on this computer; only the upload folder is public.
	if !strings.Contains(strings.Join(zipNames(t, r.FullZip), " "), "agent/claude-code/claude.exe") {
		t.Fatal("full package incomplete")
	}
	entries, _ := os.ReadDir(r.UploadDir)
	if len(entries) != 2 || filepath.Dir(r.UpdateZip) != r.UploadDir || filepath.Dir(r.Manifest) != r.UploadDir || filepath.Dir(r.FullZip) == r.UploadDir {
		t.Fatal("upload folder must hold exactly the update package and update.json", entries)
	}
	if !strings.HasPrefix(r.UploadDir, filepath.Join(dir, "release", "creative")) {
		t.Fatal(r.UploadDir)
	}
	data, _ := os.ReadFile(r.Manifest)
	m, e := update.Verify(data, []string{a.State().SigningPublicKey}, "creative")
	if e != nil || m.Version != "0.7.32" || m.Notes != "可以一键更新了" || m.NotesEn != "One-click updates are here" || m.URL != CreativeReleaseURL+filepath.Base(r.UpdateZip) {
		t.Fatal("manifest", m, e)
	}
	if _, e = update.Verify(data, []string{a.State().SigningPublicKey}, ""); e == nil {
		t.Fatal("a delivered e-commerce app would accept the creative manifest")
	}
	if got := a.State().Creative; len(got) != 1 || got[0].Version != "0.7.32" || got[0].NotesEn != "One-click updates are here" {
		t.Fatal("release not recorded", got)
	}
}

func TestCreativeReleaseRefusesOtherVersionAndMissingNotes(t *testing.T) {
	a := open(t)
	creativeRepo(t, a, "0.7.32")
	if _, e := a.BuildCreative(context.Background(), "0.7.33", "说明", "Notes", fakeBuilder{update: true}, "", func(string) {}); e == nil || !strings.Contains(e.Error(), "0.7.32") {
		t.Fatal("version differs from the source:", e)
	}
	if _, e := a.BuildCreative(context.Background(), "0.7.32", "  ", "Notes", fakeBuilder{update: true}, "", func(string) {}); e == nil {
		t.Fatal("released without notes")
	}
	if _, e := a.BuildCreative(context.Background(), "0.7.32", "说明", " ", fakeBuilder{update: true}, "", func(string) {}); e == nil || !strings.Contains(e.Error(), "英文") {
		t.Fatal("released without English notes:", e)
	}
}

// The mass-market line is built in its own build folder at the newest commit of
// main, whatever the source folder has checked out or left uncommitted.
func TestBuildsTakeTheirLinesCommitAndIgnoreTheSourceFolder(t *testing.T) {
	isolate(t)
	a := open(t)
	dir := creativeRepo(t, a, "0.7.32")
	heads := map[string]LineHead{
		CreativeBranch: {Commit: "bbbb", Short: "bbbb", Subject: "大众版提交", Version: "0.7.32", Current: CreativeBranch, Changed: []string{"frontend/src/App.tsx"}},
	}
	a.head = func(_, branch string) (LineHead, error) {
		if h, ok := heads[branch]; ok {
			return h, nil
		}
		return LineHead{}, errors.New("no such branch " + branch)
	}
	var prepared []string
	a.prepare = func(repo, line, commit string) (string, error) {
		prepared = append(prepared, line+"@"+commit)
		return filepath.Join(repo, "build-"+line), nil
	}
	var req BuildRequest
	var logs []string
	log := func(s string) { logs = append(logs, s) }
	if _, e := a.BuildCreative(context.Background(), "0.7.32", "说明", "Notes", fakeBuilder{update: true, seen: &req}, "", log); e != nil {
		t.Fatal(e)
	}
	if req.RepoRoot != dir || req.BuildDir != filepath.Join(dir, "build-creative") || strings.Join(prepared, " ") != "creative@bbbb" {
		t.Fatal(req.BuildDir, prepared)
	}
	if all := strings.Join(logs, "\n"); !strings.Contains(all, "1 个未提交的改动，不会进入这次构建") || !strings.Contains(all, "bbbb 大众版提交") {
		t.Fatal("build log lacks the commit or the notice", logs)
	}
	// A build cannot be labelled with another version than its code.
	if _, e := a.BuildCreative(context.Background(), "0.7.40", "说明", "Notes", fakeBuilder{update: true}, "", log); e == nil || !strings.Contains(e.Error(), "0.7.32") {
		t.Fatal("0.7.32 code labelled 0.7.40:", e)
	}
	// What the page shows before building; the retired e-commerce line is gone.
	if s := a.LineSource(CreativeLine); s.Version != "0.7.32" || s.Uncommitted != 1 || s.ReleasePage == "" || s.Branch != "main" {
		t.Fatalf("%+v", s)
	}
	if s := a.LineSource("ecommerce"); s.Blocked == "" {
		t.Fatalf("the e-commerce line still builds: %+v", s)
	}
	// No fallback to building in the source folder when the build folder fails.
	a.prepare = func(string, string, string) (string, error) { return "", errors.New("磁盘已满") }
	built := false
	if _, e := a.BuildCreative(context.Background(), "0.7.32", "说明", "Notes", builderFunc(func() { built = true }), "", log); e == nil || !strings.Contains(e.Error(), "磁盘已满") || built {
		t.Fatal("built without a build folder:", e, built)
	}
}

type builderFunc func()

func (f builderFunc) Build(context.Context, BuildRequest, func(string)) error { f(); return nil }

// The public update package of the mass-market line never carries Claude Code.
func TestCreativeUpdatePackageWithClaudeCodeIsRefused(t *testing.T) {
	a := open(t)
	dir := creativeRepo(t, a, "0.7.32")
	_, e := a.BuildCreative(context.Background(), "0.7.32", "说明", "Notes", fakeBuilder{update: true, agentInUpdate: true}, "", func(string) {})
	if e == nil || !strings.Contains(e.Error(), "Claude Code") {
		t.Fatal("update package with the agent accepted:", e)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "release", "creative", "0.7.32", "upload", "*")); len(left) != 0 {
		t.Fatal("refused files left in the upload folder", left)
	}
}

// The update package is public, so a key of the pool anywhere in the packages
// stops the release, as it does for customer packages.
func TestCreativeReleaseRefusesPoolKeys(t *testing.T) {
	isolate(t)
	a := open(t)
	dir := creativeRepo(t, a, "0.7.32")
	stock(t, a, 1, "http://127.0.0.1:1/v1")
	_, e := a.BuildCreative(context.Background(), "0.7.32", "说明", "Notes", fakeBuilder{update: true, leak: "sk-prim-a-00-secret"}, "", func(string) {})
	if e == nil || !strings.Contains(e.Error(), "检查未通过") {
		t.Fatal("package with a pool key accepted:", e)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "release", "creative", "0.7.32", "*.zip")); len(left) != 0 {
		t.Fatal("refused package left behind", left)
	}
}
