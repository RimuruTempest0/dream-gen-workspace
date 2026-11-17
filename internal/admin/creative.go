package admin

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"imageworkbench/internal/packcheck"
	"imageworkbench/internal/update"
)

// The mass-market line (大众版, admin 1.6.0): no licence, and updated from a fixed
// pre-release of the public repository. The admin tool builds, checks and signs;
// the provider uploads the two files of the upload folder by hand. See
// docs/adr/0001-creative-line-updates.md: the line name and these addresses are
// built into delivered programs and must not change.
const (
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
	branch, e := git(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if e != nil {
		return "", nil, errors.New("无法读取源码目录的 Git 分支（需要安装 Git，且源码目录是一个 Git 仓库）")
	}
	out, e := git(repo, "status", "--porcelain")
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

var versionInSource = regexp.MustCompile(`"productVersion"\s*:\s*"([0-9.]+)"`)

// CreativeNotes proposes the update notes for version: the opening of its entry
// in docs/CHANGELOG.md of the line's newest commit, up to the first sub-heading
// (the parts below it are about verification, not for the people using the app).
func (a *Admin) CreativeNotes(version string) string {
	a.mu.Lock()
	repo := a.d.RepoRoot
	a.mu.Unlock()
	read := a.changelog
	if read == nil {
		read = func(repo string) ([]byte, error) {
			out, e := git(repo, "show", "refs/heads/"+CreativeBranch+":docs/CHANGELOG.md")
			return []byte(out), e
		}
	}
	b, e := read(repo)
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
	work, e := a.checkout(repo, CreativeLine, version, log)
	if e != nil {
		return CreativeRelease{}, e
	}
	base := filepath.Join("release", "creative")
	// Program folders of all versions sit side by side: the build compares with
	// the previous one to decide what the update package must carry.
	rel := filepath.Join(base, "build", version)
	buildZip := filepath.Join(base, "build", "build-check.zip")
	log("开始构建大众版程序文件（约 3–5 分钟）…")
	req := BuildRequest{RepoRoot: repo, BuildDir: work, Version: version, ReleaseDir: rel, ZipPath: buildZip, UpdateURL: CreativeReleaseURL + "update.json", ProductLine: CreativeLine, UpdateKey: pub}
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
