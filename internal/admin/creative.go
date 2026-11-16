package admin

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"imageworkbench/internal/packcheck"
	"imageworkbench/internal/update"
)

// The mass-market line (大众版, admin 1.6.0): no licence, built from its own
// branch, and updated from a fixed pre-release of the public repository. The
// admin tool builds, checks and signs; the provider uploads the two files of the
// upload folder by hand. See docs/adr/0001-creative-line-updates.md: the line
// name and these addresses are built into delivered programs and must not change.
const (
	CreativeLine        = "creative"
	CreativeBranch      = "creative-concepts"
	EcommerceBranch     = "main"
	CreativeReleaseURL  = "https://github.com/RimuruTempest0/dream-gen-workspace/releases/download/creative/"
	CreativeReleasePage = "https://github.com/RimuruTempest0/dream-gen-workspace/releases/tag/creative"
)

// CreativeRelease is one built version of the mass-market line.
type CreativeRelease struct {
	Version string `json:"version"`
	At      string `json:"at"`
	// FullZip is the full package: handed to people privately, never uploaded.
	FullZip string `json:"fullZip"`
	// UploadDir holds exactly the two public files: UpdateZip and Manifest.
	UploadDir string `json:"uploadDir"`
	UpdateZip string `json:"updateZip"`
	Manifest  string `json:"manifest"`
	Notes     string `json:"notes"`
}

// gitRepoState reports the checked-out branch of repo and the paths with
// uncommitted changes. Untracked files under .claude/ are local agent tooling,
// not part of either product line, and are left out.
func gitRepoState(repo string) (string, []string, error) {
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "core.quotepath=false"}, args...)...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, e := cmd.Output()
		return strings.TrimRight(string(out), "\r\n"), e
	}
	branch, e := git("rev-parse", "--abbrev-ref", "HEAD")
	if e != nil {
		return "", nil, errors.New("无法读取源码目录的 Git 分支（需要安装 Git，且源码目录是一个 Git 仓库）")
	}
	out, e := git("status", "--porcelain")
	if e != nil {
		return "", nil, errors.New("无法读取源码目录的改动情况")
	}
	var changed []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.Trim(strings.TrimSpace(line[3:]), `"`)
		if strings.HasPrefix(line, "??") && strings.HasPrefix(path, ".claude/") {
			continue
		}
		changed = append(changed, path)
	}
	return branch, changed, nil
}

// requireBranch refuses a build from the wrong product line's branch or with
// uncommitted changes, so that what ships is exactly a commit of that line.
func (a *Admin) requireBranch(repo, want, what string) error {
	state := a.repoState
	if state == nil {
		state = gitRepoState
	}
	branch, changed, e := state(repo)
	if e != nil {
		return e
	}
	return branchProblem(branch, changed, want, what)
}

func branchProblem(branch string, changed []string, want, what string) error {
	if branch != want {
		return fmt.Errorf("%s要从 %s 分支构建，源码目录现在是 %s 分支。请先切换分支再试", what, want, branch)
	}
	if len(changed) > 0 {
		list := changed
		more := ""
		if len(list) > 5 {
			list, more = list[:5], fmt.Sprintf(" 等 %d 个文件", len(changed))
		}
		return fmt.Errorf("源码目录有未提交的改动（%s%s）。请先提交或撤销，再构建%s", strings.Join(list, "、"), more, what)
	}
	return nil
}

var versionInSource = regexp.MustCompile(`"productVersion"\s*:\s*"([0-9.]+)"`)

// SourceVersion is the app version of the source folder (wails.json).
func SourceVersion(repo string) string {
	b, e := os.ReadFile(filepath.Join(repo, "wails.json"))
	if e != nil {
		return ""
	}
	if m := versionInSource.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// CreativeNotes proposes the update notes for version: the opening of its entry
// in docs/CHANGELOG.md, up to the first sub-heading (the parts below it are
// about verification, not for the people using the app).
func (a *Admin) CreativeNotes(version string) string {
	a.mu.Lock()
	repo := a.d.RepoRoot
	a.mu.Unlock()
	b, e := os.ReadFile(filepath.Join(repo, "docs", "CHANGELOG.md"))
	if e != nil {
		return ""
	}
	var out []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(string(update.TrimBOM(b)), "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			if in {
				return strings.TrimSpace(strings.Join(out, "\n"))
			}
			head := strings.Fields(strings.TrimPrefix(line, "## "))
			in = len(head) > 0 && head[0] == version
		case in && strings.HasPrefix(line, "### "):
			return strings.TrimSpace(strings.Join(out, "\n"))
		case in:
			out = append(out, strings.ReplaceAll(line, "**", ""))
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// BuildCreative builds the mass-market line at version, checks both packages and
// signs update.json. Afterwards release/creative/<version>/ holds the full
// package and upload/ with the two files to publish.
func (a *Admin) BuildCreative(ctx context.Context, version, notes string, builder Builder, localDataDir string, log func(string)) (CreativeRelease, error) {
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return CreativeRelease{}, errors.New("请填写更新说明，顾客在更新前会看到它")
	}
	a.mu.Lock()
	repo, pub := a.d.RepoRoot, a.d.SigningPublicKey
	// The update package is public: no key of the pool may be in either package.
	secrets := []packcheck.Secret{}
	for i := range a.d.Keys {
		if v, e := a.secret(a.d.Keys[i].SecretRef); e == nil {
			secrets = append(secrets, packcheck.Secret{Kind: "Key 池里的 Key", Value: string(v)})
		}
	}
	a.mu.Unlock()
	if repo == "" {
		return CreativeRelease{}, errors.New("请先在“设置”中选择软件源码目录")
	}
	if pub == "" {
		return CreativeRelease{}, errors.New("还没有签名密钥")
	}
	if e := a.requireBranch(repo, CreativeBranch, "大众版"); e != nil {
		return CreativeRelease{}, e
	}
	if src := SourceVersion(repo); src != version {
		return CreativeRelease{}, fmt.Errorf("源码里的版本号是 %s，与要发布的 %s 不一致。大众版按源码的版本号发布", src, version)
	}
	base := filepath.Join("release", "creative")
	// Program folders of all versions sit side by side: the build compares with
	// the previous one to decide what the update package must carry.
	rel := filepath.Join(base, "build", version)
	buildZip := filepath.Join(base, "build", "build-check.zip")
	log("开始构建大众版程序文件（约 3–5 分钟）…")
	req := BuildRequest{RepoRoot: repo, Version: version, ReleaseDir: rel, ZipPath: buildZip, UpdateURL: CreativeReleaseURL + "update.json", ProductLine: CreativeLine, UpdateKey: pub}
	if e := builder.Build(ctx, req, log); e != nil {
		return CreativeRelease{}, fmt.Errorf("构建失败：%w", e)
	}
	os.Remove(filepath.Join(repo, buildZip))
	builtUpdate := filepath.Join(repo, strings.TrimSuffix(buildZip, ".zip")+"-update.zip")
	defer os.Remove(builtUpdate)
	if !fileExists(builtUpdate) {
		return CreativeRelease{}, errors.New("构建没有产出程序更新包")
	}
	out := filepath.Join(repo, base, version)
	upload := filepath.Join(out, "upload")
	if e := os.RemoveAll(upload); e != nil {
		return CreativeRelease{}, e
	}
	if e := os.MkdirAll(upload, 0o755); e != nil {
		return CreativeRelease{}, e
	}
	r := CreativeRelease{Version: version, At: now(), Notes: notes, UploadDir: upload,
		FullZip:   filepath.Join(out, fmt.Sprintf("ImageWorkbench-%s.zip", version)),
		UpdateZip: filepath.Join(upload, fmt.Sprintf("ImageWorkbench-update-%s.zip", version)),
		Manifest:  filepath.Join(upload, "update.json")}
	fail := func(e error) (CreativeRelease, error) {
		os.RemoveAll(upload)
		os.Remove(r.FullZip)
		return CreativeRelease{}, e
	}
	check := func(path, what string) error {
		findings, e := packcheck.Check(path, packcheck.Options{DataDir: localDataDir, Secrets: secrets})
		if e == nil && len(findings) > 0 {
			e = errors.New(strings.Join(findings, "；"))
		}
		if e != nil {
			return fmt.Errorf("%s检查未通过，已删除：%w", what, e)
		}
		return nil
	}
	if e := os.Rename(builtUpdate, r.UpdateZip); e != nil {
		return fail(e)
	}
	log("检查更新包：只含程序文件、属于大众版、不带 Claude Code、没有本机连接和 Key 池里的 Key …")
	if e := checkCreativeUpdate(r.UpdateZip, version); e != nil {
		return fail(e)
	}
	if e := check(r.UpdateZip, "更新包"); e != nil {
		return fail(e)
	}
	log("打包完整包 " + filepath.Base(r.FullZip) + " …")
	if e := writeZipOf(filepath.Join(repo, rel), r.FullZip, programEntries); e != nil {
		return fail(e)
	}
	if e := check(r.FullZip, "完整包"); e != nil {
		return fail(e)
	}
	sum, e := update.FileSHA256(r.UpdateZip)
	if e != nil {
		return fail(e)
	}
	info, e := os.Stat(r.UpdateZip)
	if e != nil {
		return fail(e)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	priv, e := a.signingKeyLocked()
	if e != nil {
		return fail(e)
	}
	data, e := update.Sign(update.Manifest{Line: CreativeLine, Version: version, Notes: notes, URL: CreativeReleaseURL + filepath.Base(r.UpdateZip), SHA256: sum, Size: info.Size()}, priv)
	if e == nil {
		_, e = update.Verify(data, []string{pub}, CreativeLine)
	}
	if e == nil {
		e = os.WriteFile(r.Manifest, data, 0o644)
	}
	if e != nil {
		return fail(e)
	}
	kept := a.d.Creative[:0]
	for _, old := range a.d.Creative {
		if old.Version != version {
			kept = append(kept, old)
		}
	}
	a.d.Creative = append(kept, r)
	log("完成。要上传的两个文件在：" + upload)
	return r, a.saveLocked()
}

// checkCreativeUpdate makes sure a package is a mass-market update package of
// version: program files only, built for the line, without the bundled Claude
// Code (docs/adr/0001).
func checkCreativeUpdate(path, version string) error {
	got, e := packageVersion(path)
	if e != nil {
		return e
	}
	if got != version {
		return fmt.Errorf("更新包的版本是 %s，不是 %s", got, version)
	}
	z, e := zip.OpenReader(path)
	if e != nil {
		return e
	}
	defer z.Close()
	for _, f := range z.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if strings.HasPrefix(name, "agent/") {
			return errors.New("大众版的更新包不能带内置 Claude Code（" + name + "），已删除")
		}
	}
	dir, e := os.MkdirTemp("", "iw-creative-check")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	stage := filepath.Join(dir, "p")
	if e = update.Extract(path, stage); e != nil {
		return e
	}
	info, e := update.ReadInfo(filepath.Join(stage, "version.json"))
	if e != nil || info.Line != CreativeLine {
		return errors.New("这个更新包不是大众版的构建，已删除")
	}
	return nil
}

// CreativeSource is what the "发布大众版" page shows before building.
type CreativeSource struct {
	Version string   `json:"version"` // wails.json of the source folder
	Branch  string   `json:"branch"`
	Changed []string `json:"changed"`
	// Blocked is why a build would be refused now, or "".
	Blocked     string `json:"blocked"`
	ReleasePage string `json:"releasePage"`
	ReleaseURL  string `json:"releaseURL"`
}

// CreativeSource reads the state of the source folder for the mass-market line.
func (a *Admin) CreativeSource() CreativeSource {
	a.mu.Lock()
	repo := a.d.RepoRoot
	a.mu.Unlock()
	s := CreativeSource{Changed: []string{}, ReleasePage: CreativeReleasePage, ReleaseURL: CreativeReleaseURL}
	if repo == "" {
		s.Blocked = "请先在“设置与签名密钥”中选择软件源码目录"
		return s
	}
	s.Version = SourceVersion(repo)
	state := a.repoState
	if state == nil {
		state = gitRepoState
	}
	branch, changed, e := state(repo)
	if e != nil {
		s.Blocked = e.Error()
		return s
	}
	s.Branch = branch
	if changed != nil {
		s.Changed = changed
	}
	if e = branchProblem(branch, changed, CreativeBranch, "大众版"); e != nil {
		s.Blocked = e.Error()
	}
	return s
}
