package admin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Each product line is built in a folder of its own next to the source folder
// (<源码目录>-build\<line>, a Git worktree), set to the newest commit of the
// line's branch before every build. The source folder itself is only read: the
// admin tool never switches its branch, commits, fetches or pushes there. See
// docs/adr/0002-build-folders-per-product-line.md.
const (
	CreativeLine   = "creative"
	CreativeBranch = "main"
)

// lineBranch is the branch a product line is built from. The e-commerce line
// was retired in admin 1.12.0 (docs/adr/0005-retire-ecommerce-line.md): the
// mass-market line is the only one left.
func lineBranch(line string) (string, error) {
	if line == CreativeLine {
		return CreativeBranch, nil
	}
	return "", fmt.Errorf("没有 %q 这条产品线", line)
}

func lineName(string) string { return "大众版" }

// Folders of the source folder that Git does not track and a build needs: the
// tool chain, the installed frontend packages, and where everything built goes.
// The build folder reaches them through directory links, so nothing is copied
// and what is built lands in the source folder's release\ as before.
var sharedFolders = []string{".tools", filepath.Join("frontend", "node_modules"), "release"}

const buildFoldersNote = "此文件夹由 ImageWorkbench 管理工具维护。\r\n\r\n" +
	"里面每个子文件夹是一条产品线的构建目录，每次构建前都会被恢复成该产品线分支的最新提交：\r\n" +
	"在这里做的任何改动都会被丢弃，请到源码目录里改。\r\n\r\n" +
	".tools、frontend\\node_modules 和 release 是指向源码目录的链接，不是副本。\r\n" +
	"不要在资源管理器里删除它们里面的内容；要清理构建目录，删除整个子文件夹之前先删这三个链接本身。\r\n"

// LineHead is the newest commit of a product line's branch, read from the
// source folder without checking it out.
type LineHead struct {
	Commit  string // full hash
	Short   string
	Subject string
	At      string // commit time, RFC 3339
	Version string // wails.json of that commit
	// Current is the branch checked out in the source folder, Changed its
	// uncommitted paths. They never enter a build.
	Current string
	Changed []string
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "core.quotepath=false"}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := cmd.Output()
	if e != nil {
		var x *exec.ExitError
		if errors.As(e, &x) && len(x.Stderr) > 0 {
			e = errors.New(strings.TrimSpace(string(x.Stderr)))
		}
	}
	return strings.TrimRight(string(out), "\r\n"), e
}

// gitLineHead reads the newest commit of branch in the source folder repo.
func gitLineHead(repo, branch string) (LineHead, error) {
	var h LineHead
	current, changed, e := gitRepoState(repo)
	if e != nil {
		return h, e
	}
	h.Current, h.Changed = current, changed
	if h.Commit, e = git(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}"); e != nil || h.Commit == "" {
		return h, fmt.Errorf("源码目录里没有 %s 分支", branch)
	}
	out, e := git(repo, "log", "-1", "--format=%h%x1f%s%x1f%cI", h.Commit)
	if e != nil {
		return h, errors.New("无法读取 " + branch + " 分支的最新提交")
	}
	if p := strings.SplitN(out, "\x1f", 3); len(p) == 3 {
		h.Short, h.Subject, h.At = p[0], p[1], p[2]
	}
	if out, e = git(repo, "show", h.Commit+":wails.json"); e == nil {
		if m := versionInSource.FindStringSubmatch(out); m != nil {
			h.Version = m[1]
		}
	}
	if h.Version == "" {
		return h, errors.New(branch + " 分支最新提交的 wails.json 里没有版本号")
	}
	return h, nil
}

// buildFolder is where line is built for the source folder repo.
func buildFolder(repo, line string) string {
	return filepath.Join(filepath.Clean(repo)+"-build", line)
}

func isLink(path string) bool {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return false
	}
	attrs, e := syscall.GetFileAttributes(p)
	return e == nil && attrs&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

// unlink takes away a directory link without touching what it points at. A real
// folder in its place is only removed when empty: the admin tool never deletes
// tool chains or built packages.
func unlink(path string) error {
	if _, e := os.Lstat(path); e != nil {
		return nil
	}
	// os.Remove on a directory link removes the link alone (RemoveDirectory).
	if e := os.Remove(path); e != nil {
		if isLink(path) {
			return fmt.Errorf("无法移除构建目录里的链接 %s：%w", path, e)
		}
		return fmt.Errorf("构建目录里的 %s 是一个有内容的真实文件夹，不是指向源码目录的链接。管理工具不会删除它，请查看后手动处理", path)
	}
	return nil
}

// removeTree deletes a folder without ever following a link out of it.
func removeTree(path string) error {
	if _, e := os.Lstat(path); e != nil {
		return nil
	}
	if isLink(path) {
		return os.Remove(path)
	}
	entries, e := os.ReadDir(path)
	if e != nil {
		// A file.
		os.Chmod(path, 0o666)
		return os.Remove(path)
	}
	for _, entry := range entries {
		if e = removeTree(filepath.Join(path, entry.Name())); e != nil {
			return e
		}
	}
	return os.Remove(path)
}

func link(path, target string) error {
	if e := os.MkdirAll(filepath.Dir(path), 0o755); e != nil {
		return e
	}
	// A junction: needs no administrator rights, unlike a symbolic link.
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CmdLine: fmt.Sprintf(`cmd.exe /c mklink /J "%s" "%s"`, path, target)}
	if out, e := cmd.CombinedOutput(); e != nil || !isLink(path) {
		return fmt.Errorf("无法在构建目录里建立到 %s 的链接：%s", target, strings.TrimSpace(string(out)))
	}
	return nil
}

func samePath(a, b string) bool {
	x, e1 := filepath.Abs(filepath.FromSlash(a))
	y, e2 := filepath.Abs(filepath.FromSlash(b))
	return e1 == nil && e2 == nil && strings.EqualFold(filepath.Clean(x), filepath.Clean(y))
}

// isWorktreeOf reports whether dir is a working folder of the repository in repo.
func isWorktreeOf(dir, repo string) bool {
	top, e := git(dir, "rev-parse", "--show-toplevel")
	if e != nil || !samePath(top, dir) {
		return false
	}
	mine, e1 := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	theirs, e2 := git(repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	return e1 == nil && e2 == nil && samePath(mine, theirs)
}

// prepareBuildFolder makes the build folder of line hold exactly commit and
// returns it. Anything changed in the folder since the last build is discarded:
// it is the admin tool's own working space. Nothing in repo changes except
// Git's list of working folders.
func prepareBuildFolder(repo, line, commit string) (string, error) {
	dir := buildFolder(repo, line)
	fail := func(e error) (string, error) {
		return "", fmt.Errorf("无法准备%s的构建目录 %s：%w", lineName(line), dir, e)
	}
	if e := os.MkdirAll(filepath.Dir(dir), 0o755); e != nil {
		return fail(e)
	}
	os.WriteFile(filepath.Join(filepath.Dir(dir), "说明.txt"), []byte("\xef\xbb\xbf"+buildFoldersNote), 0o644)
	// The links go first, so that no Git command below can reach through them
	// into the source folder's tool chain or packages.
	for _, name := range sharedFolders {
		if e := unlink(filepath.Join(dir, name)); e != nil {
			return "", e
		}
	}
	if isWorktreeOf(dir, repo) {
		for _, args := range [][]string{{"checkout", "--quiet", "--detach", "--force", commit}, {"reset", "--quiet", "--hard", commit}, {"clean", "--quiet", "-fd"}} {
			if _, e := git(dir, args...); e != nil {
				return fail(e)
			}
		}
	} else {
		if e := removeTree(dir); e != nil {
			return fail(e)
		}
		git(repo, "worktree", "prune")
		if _, e := git(repo, "worktree", "add", "--quiet", "--detach", "--force", dir, commit); e != nil {
			return fail(e)
		}
	}
	if got, e := git(dir, "rev-parse", "HEAD"); e != nil || got != commit {
		return fail(fmt.Errorf("构建目录没有对准提交 %s", commit))
	}
	for _, name := range sharedFolders {
		target := filepath.Join(repo, name)
		if name == "release" {
			if e := os.MkdirAll(target, 0o755); e != nil {
				return fail(e)
			}
		} else if _, e := os.Stat(target); e != nil {
			continue
		}
		if e := link(filepath.Join(dir, name), target); e != nil {
			return fail(e)
		}
	}
	return dir, nil
}

// LineSource is what a build page shows before building a product line.
type LineSource struct {
	Line    string `json:"line"`
	Branch  string `json:"branch"`
	Version string `json:"version"`
	Commit  string `json:"commit"` // short hash
	Subject string `json:"subject"`
	At      string `json:"at"`
	// Uncommitted is how many paths of the source folder have uncommitted
	// changes while it is on this line's branch: they will not be in the build.
	Uncommitted int `json:"uncommitted"`
	// Blocked is why a build would be refused now, or "".
	Blocked     string `json:"blocked"`
	ReleasePage string `json:"releasePage,omitempty"`
	ReleaseURL  string `json:"releaseURL,omitempty"`
}

func (a *Admin) lineHead(repo, branch string) (LineHead, error) {
	if a.head != nil {
		return a.head(repo, branch)
	}
	return gitLineHead(repo, branch)
}

// LineSource reads which commit a build of line would take.
func (a *Admin) LineSource(line string) LineSource {
	s := LineSource{Line: line}
	if line == CreativeLine {
		s.ReleasePage, s.ReleaseURL = CreativeReleasePage, CreativeReleaseURL
	}
	branch, e := lineBranch(line)
	if e != nil {
		s.Blocked = e.Error()
		return s
	}
	s.Branch = branch
	a.mu.Lock()
	repo := a.d.RepoRoot
	a.mu.Unlock()
	if repo == "" {
		s.Blocked = "请先在“设置与签名密钥”中选择软件源码目录"
		return s
	}
	h, e := a.lineHead(repo, branch)
	if e != nil {
		s.Blocked = e.Error()
		return s
	}
	s.Version, s.Commit, s.Subject, s.At = h.Version, h.Short, h.Subject, h.At
	if h.Current == branch {
		s.Uncommitted = len(h.Changed)
	}
	return s
}

// checkout readies the build folder of line at the newest commit of its branch
// and returns the folder. version must be the version of that commit: a package
// is always labelled with the version of the code inside it.
func (a *Admin) checkout(repo, line, version string, log func(string)) (string, error) {
	branch, e := lineBranch(line)
	if e != nil {
		return "", e
	}
	h, e := a.lineHead(repo, branch)
	if e != nil {
		return "", e
	}
	if h.Version != version {
		return "", fmt.Errorf("%s源码（%s 分支）的版本号是 %s，与要构建的 %s 不一致。%s按源码的版本号构建", lineName(line), branch, h.Version, version, lineName(line))
	}
	log(fmt.Sprintf("构建%s：%s 分支的提交 %s %s", lineName(line), branch, h.Short, h.Subject))
	if h.Current == branch && len(h.Changed) > 0 {
		log(fmt.Sprintf("源码目录有 %d 个未提交的改动，不会进入这次构建。", len(h.Changed)))
	}
	prepare := a.prepare
	if prepare == nil {
		prepare = prepareBuildFolder
	}
	dir, e := prepare(repo, line, h.Commit)
	if e != nil {
		return "", e
	}
	log("构建目录：" + dir)
	return dir, nil
}
