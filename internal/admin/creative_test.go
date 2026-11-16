package admin

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imageworkbench/internal/update"
)

func onBranch(a *Admin, branch string, changed ...string) {
	a.repoState = func(string) (string, []string, error) { return branch, changed, nil }
}

func creativeRepo(t *testing.T, a *Admin, version string) string {
	t.Helper()
	dir := repo(t, a)
	os.WriteFile(filepath.Join(dir, "wails.json"), []byte(`{"info": {"productVersion": "`+version+`"}}`), 0o644)
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	os.WriteFile(filepath.Join(dir, "docs", "CHANGELOG.md"), []byte("# 更新记录\r\n\r\n## "+version+" · 一键更新\r\n\r\n大众版可以一键更新。\r\n\r\n- 点一次即可\r\n\r\n### 验证\r\n- 内部说明\r\n\r\n## 0.0.1 · 旧\r\n\r\n旧说明\r\n"), 0o644)
	onBranch(a, "creative-concepts")
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
	r, e := a.BuildCreative(context.Background(), "0.7.32", "可以一键更新了", fakeBuilder{update: true, seen: &req}, "", func(string) {})
	if e != nil {
		t.Fatal(e)
	}
	if req.ProductLine != "creative" || req.UpdateKey != a.State().SigningPublicKey || req.PublicKey != "" || req.UpdateURL != CreativeReleaseURL+"update.json" {
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
	if e != nil || m.Version != "0.7.32" || m.Notes != "可以一键更新了" || m.URL != CreativeReleaseURL+filepath.Base(r.UpdateZip) {
		t.Fatal("manifest", m, e)
	}
	if _, e = update.Verify(data, []string{a.State().SigningPublicKey}, ""); e == nil {
		t.Fatal("a delivered e-commerce app would accept the creative manifest")
	}
	if got := a.State().Creative; len(got) != 1 || got[0].Version != "0.7.32" {
		t.Fatal("release not recorded", got)
	}
}

func TestCreativeReleaseRefusesWrongBranchDirtyTreeAndVersion(t *testing.T) {
	a := open(t)
	creativeRepo(t, a, "0.7.32")
	build := func() error {
		_, e := a.BuildCreative(context.Background(), "0.7.32", "说明", fakeBuilder{update: true}, "", func(string) {})
		return e
	}
	onBranch(a, "main")
	if e := build(); e == nil || !strings.Contains(e.Error(), "creative-concepts") {
		t.Fatal("built the mass-market line from main:", e)
	}
	onBranch(a, "creative-concepts", "frontend/src/App.tsx")
	if e := build(); e == nil || !strings.Contains(e.Error(), "frontend/src/App.tsx") {
		t.Fatal("built with uncommitted changes:", e)
	}
	onBranch(a, "creative-concepts")
	if _, e := a.BuildCreative(context.Background(), "0.7.33", "说明", fakeBuilder{update: true}, "", func(string) {}); e == nil || !strings.Contains(e.Error(), "0.7.32") {
		t.Fatal("version differs from the source:", e)
	}
	if _, e := a.BuildCreative(context.Background(), "0.7.32", "  ", fakeBuilder{update: true}, "", func(string) {}); e == nil {
		t.Fatal("released without notes")
	}
	if e := build(); e != nil {
		t.Fatal(e)
	}
}

// The public update package of the mass-market line never carries Claude Code.
func TestCreativeUpdatePackageWithClaudeCodeIsRefused(t *testing.T) {
	a := open(t)
	dir := creativeRepo(t, a, "0.7.32")
	_, e := a.BuildCreative(context.Background(), "0.7.32", "说明", fakeBuilder{update: true, agentInUpdate: true}, "", func(string) {})
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
	_, e := a.BuildCreative(context.Background(), "0.7.32", "说明", fakeBuilder{update: true, leak: "sk-prim-a-00-secret"}, "", func(string) {})
	if e == nil || !strings.Contains(e.Error(), "检查未通过") {
		t.Fatal("package with a pool key accepted:", e)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "release", "creative", "0.7.32", "*.zip")); len(left) != 0 {
		t.Fatal("refused package left behind", left)
	}
}

// Customer packages are the e-commerce line: built from main, nothing uncommitted.
func TestCustomerPackageNeedsMainBranch(t *testing.T) {
	isolate(t)
	a := open(t)
	repo(t, a)
	stock(t, a, 1, "http://127.0.0.1:1/v1")
	c, _ := a.CreateCustomer("客户 D", 3)
	a.AutoAssign(c.ID)
	onBranch(a, "creative-concepts")
	if _, e := a.BuildPackage(context.Background(), c.ID, "0.7.17", fakeBuilder{}, "", func(string) {}); e == nil || !strings.Contains(e.Error(), "main") {
		t.Fatal("customer package built from the mass-market branch:", e)
	}
	onBranch(a, "main", "scripts/build.ps1")
	if _, e := a.BuildPackage(context.Background(), c.ID, "0.7.17", fakeBuilder{}, "", func(string) {}); e == nil || !strings.Contains(e.Error(), "未提交") {
		t.Fatal("customer package built with uncommitted changes:", e)
	}
	onBranch(a, "main")
	if _, e := a.BuildPackage(context.Background(), c.ID, "0.7.17", fakeBuilder{}, "", func(string) {}); e != nil {
		t.Fatal(e)
	}
}
