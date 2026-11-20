package admin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sourceRepo is a real Git repository shaped like the source folder: the
// mass-market line on main, a work branch ahead of it that is checked out, and
// the untracked folders a build needs.
func sourceRepo(t *testing.T) string {
	t.Helper()
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("git is not installed")
	}
	dir := filepath.Join(t.TempDir(), "源码 目录")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "core.autocrlf=false"}, args...)...)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(args, string(out), e)
		}
	}
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if e := os.WriteFile(p, []byte(body), 0o644); e != nil {
			t.Fatal(e)
		}
	}
	os.MkdirAll(dir, 0o755)
	run("init", "-q", "-b", "main")
	write(".gitignore", ".tools/\nrelease/\nfrontend/node_modules/\n")
	write("scripts/build.ps1", "# creative")
	write("wails.json", `{"info": {"productVersion": "0.7.33"}}`)
	write("docs/CHANGELOG.md", "# 更新记录\n\n## 0.7.33 · 打磨\n\n主路径更顺了。\n")
	run("add", "-A")
	run("commit", "-q", "-m", "大众版 0.7.33")
	run("checkout", "-q", "-b", "work")
	write("scripts/build.ps1", "# work")
	write("wails.json", `{"info": {"productVersion": "0.7.40"}}`)
	run("add", "-A")
	run("commit", "-q", "-m", "进行中的功能")
	write(".tools/node.ps1", "tool chain")
	write("frontend/node_modules/vite/package.json", "{}")
	write("release/customers/abc/0.7.17/data/app.db", "customer test data")
	return dir
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

func TestBuildFolderHoldsTheLinesCommitAndLeavesTheSourceFolderAlone(t *testing.T) {
	repo := sourceRepo(t)
	// Work in progress in the source folder, on a work branch.
	os.WriteFile(filepath.Join(repo, "wails.json"), []byte(`{"info": {"productVersion": "9.9.9"}}`), 0o644)
	os.WriteFile(filepath.Join(repo, "scratch.txt"), []byte("not committed"), 0o644)

	creative, e := gitLineHead(repo, CreativeBranch)
	// The version is the committed one of main, not the work branch's or the edited file's.
	if e != nil || creative.Version != "0.7.33" || creative.Subject != "大众版 0.7.33" || creative.Current != "work" || len(creative.Changed) != 2 {
		t.Fatalf("%+v %v", creative, e)
	}
	if _, e = gitLineHead(repo, "no-such-branch"); e == nil {
		t.Fatal("a missing branch has a newest commit")
	}

	dir, e := prepareBuildFolder(repo, CreativeLine, creative.Commit)
	if e != nil {
		t.Fatal(e)
	}
	if dir != repo+`-build\creative` {
		t.Fatal(dir)
	}
	if read(t, filepath.Join(dir, "scripts", "build.ps1")) != "# creative" || !strings.Contains(read(t, filepath.Join(dir, "wails.json")), "0.7.33") {
		t.Fatal("the build folder does not hold the commit of main")
	}
	// The tool chain, the frontend packages and release\ are the source folder's own.
	if read(t, filepath.Join(dir, ".tools", "node.ps1")) != "tool chain" || read(t, filepath.Join(dir, "frontend", "node_modules", "vite", "package.json")) != "{}" {
		t.Fatal("the build folder cannot reach the tool chain or the frontend packages")
	}
	os.WriteFile(filepath.Join(dir, "release", "built.zip"), []byte("package"), 0o644)
	if read(t, filepath.Join(repo, "release", "built.zip")) != "package" {
		t.Fatal("what is built does not land in the source folder's release folder")
	}
	if !strings.Contains(read(t, filepath.Join(repo+"-build", "说明.txt")), "改动都会被丢弃") {
		t.Fatal("no notice in the build folders")
	}

	// Changes made in a build folder are discarded by the next build: edited,
	// deleted and added files alike.
	os.WriteFile(filepath.Join(dir, "scripts", "build.ps1"), []byte("# tampered"), 0o644)
	os.Remove(filepath.Join(dir, "wails.json"))
	os.WriteFile(filepath.Join(dir, "extra.go"), []byte("package x"), 0o644)
	if _, e = prepareBuildFolder(repo, CreativeLine, creative.Commit); e != nil {
		t.Fatal(e)
	}
	if read(t, filepath.Join(dir, "scripts", "build.ps1")) != "# creative" || !fileExists(filepath.Join(dir, "wails.json")) || fileExists(filepath.Join(dir, "extra.go")) {
		t.Fatal("a tampered build folder was built from")
	}

	// A build folder that is no longer a working folder of the repository (deleted
	// by hand, or left over from another source folder) is made again.
	os.Remove(filepath.Join(dir, ".git"))
	if _, e = prepareBuildFolder(repo, CreativeLine, creative.Commit); e != nil || read(t, filepath.Join(dir, "scripts", "build.ps1")) != "# creative" {
		t.Fatal("a broken build folder was not made again:", e)
	}

	// Through all of this the source folder kept its branch, its uncommitted work
	// and everything under the linked folders.
	branch, changed, e := gitRepoState(repo)
	if e != nil || branch != "work" || strings.Join(changed, " ") != "wails.json scratch.txt" {
		t.Fatal("the source folder changed:", branch, changed, e)
	}
	if !strings.Contains(read(t, filepath.Join(repo, "wails.json")), "9.9.9") || read(t, filepath.Join(repo, "scratch.txt")) != "not committed" {
		t.Fatal("uncommitted work in the source folder was lost")
	}
	for _, kept := range []string{".tools/node.ps1", "frontend/node_modules/vite/package.json", "release/customers/abc/0.7.17/data/app.db", "release/built.zip"} {
		if !fileExists(filepath.Join(repo, filepath.FromSlash(kept))) {
			t.Fatal("lost from the source folder:", kept)
		}
	}
}

// The admin tool never deletes a real folder of packages or tools: if one sits
// where a link belongs in the build folder, the build stops.
func TestBuildFolderRefusesARealFolderInPlaceOfALink(t *testing.T) {
	repo := sourceRepo(t)
	h, _ := gitLineHead(repo, CreativeBranch)
	dir, e := prepareBuildFolder(repo, CreativeLine, h.Commit)
	if e != nil {
		t.Fatal(e)
	}
	os.Remove(filepath.Join(dir, "release"))
	os.MkdirAll(filepath.Join(dir, "release", "kept"), 0o755)
	os.WriteFile(filepath.Join(dir, "release", "kept", "package.zip"), []byte("x"), 0o644)
	if _, e = prepareBuildFolder(repo, CreativeLine, h.Commit); e == nil || !strings.Contains(e.Error(), "不会删除") {
		t.Fatal("built over a real release folder:", e)
	}
	if !fileExists(filepath.Join(dir, "release", "kept", "package.zip")) {
		t.Fatal("deleted a real folder's content")
	}
}
