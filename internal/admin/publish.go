package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"imageworkbench/internal/update"
)

// Uploading a mass-market release (admin 1.11.0). The tool puts the two files
// of the upload folder on the fixed creative pre-release through the GitHub CLI
// (gh) the provider is signed in to; it keeps no GitHub credentials itself.
// Guards: the release must stay a pre-release and must not be Latest (delivered
// e-commerce apps read Latest), the package goes up before update.json, a
// version never replaces a newer one, and what GitHub then serves is read back.

const (
	CreativeRepo = "RimuruTempest0/dream-gen-workspace"
	CreativeTag  = "creative"
)

// ErrNotPublished: the release holds no such file yet.
var ErrNotPublished = errors.New("not published")

// HostRelease is what the creative release looks like now.
type HostRelease struct {
	Prerelease bool
	Latest     bool
	Assets     []string
}

// ReleaseHost is the creative release on GitHub; tests and desktop QA replace it.
type ReleaseHost interface {
	Release(ctx context.Context) (HostRelease, error)
	// Upload adds or replaces the file under its own name.
	Upload(ctx context.Context, path string) error
	// Fetch downloads a file the way customers' apps do (public address).
	Fetch(ctx context.Context, name string) ([]byte, error)
}

func baseName(path string) string { return filepath.Base(path) }

// UploadFile is one file of an upload, for the confirmation.
type UploadFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// UploadPlan is what the confirmation shows before anything is sent.
type UploadPlan struct {
	Version string       `json:"version"`
	Files   []UploadFile `json:"files"`
	Page    string       `json:"page"`
	Tag     string       `json:"tag"`
	// Published is the version update.json on the release announces now ("" = none).
	Published string `json:"published"`
}

// creativeRecord finds a built version and checks that its two files are still
// the ones that were signed: update.json verifies for the line and version, and
// the package matches its size and SHA-256.
func (a *Admin) creativeRecord(version string) (CreativeRelease, []byte, error) {
	a.mu.Lock()
	pub := a.d.SigningPublicKey
	var r CreativeRelease
	found := false
	for _, c := range a.d.Creative {
		if c.Version == version {
			r, found = c, true
		}
	}
	a.mu.Unlock()
	if !found {
		return r, nil, fmt.Errorf("还没有构建过大众版 %s", version)
	}
	data, e := os.ReadFile(r.Manifest)
	if e != nil {
		return r, nil, errors.New("找不到这一版的 update.json，请重新构建")
	}
	m, e := update.Verify(data, []string{pub}, CreativeLine)
	if e != nil || m.Version != version {
		return r, nil, errors.New("这一版的 update.json 无法用签名公钥验证，请重新构建")
	}
	info, e := os.Stat(r.UpdateZip)
	if e != nil {
		return r, nil, errors.New("找不到这一版的更新包，请重新构建")
	}
	sum, e := update.FileSHA256(r.UpdateZip)
	if e != nil || info.Size() != m.Size || !strings.EqualFold(sum, m.SHA256) {
		return r, nil, errors.New("更新包在构建之后被改动过，和 update.json 对不上，请重新构建")
	}
	if m.URL != CreativeReleaseURL+filepath.Base(r.UpdateZip) {
		return r, nil, errors.New("update.json 里的下载地址不是 creative Release，请重新构建")
	}
	return r, data, nil
}

// publishedVersion reads the version update.json on the release announces now.
func (a *Admin) publishedVersion(ctx context.Context, host ReleaseHost) (string, error) {
	data, e := host.Fetch(ctx, "update.json")
	if errors.Is(e, ErrNotPublished) {
		return "", nil
	}
	if e != nil {
		return "", fmt.Errorf("读取 Release 上现有的 update.json 失败：%w", e)
	}
	a.mu.Lock()
	pub := a.d.SigningPublicKey
	a.mu.Unlock()
	m, e := update.Verify(data, []string{pub}, CreativeLine)
	if e != nil {
		return "", errors.New("Release 上现有的 update.json 不是这把签名密钥签的大众版清单，请先到网页上看一下")
	}
	return m.Version, nil
}

// CreativeUploadPlan lists what UploadCreative would send, for the confirmation.
func (a *Admin) CreativeUploadPlan(ctx context.Context, version string, host ReleaseHost) (UploadPlan, error) {
	r, _, e := a.creativeRecord(version)
	if e != nil {
		return UploadPlan{}, e
	}
	p := UploadPlan{Version: version, Page: CreativeReleasePage, Tag: CreativeTag}
	for _, path := range []string{r.UpdateZip, r.Manifest} {
		info, e := os.Stat(path)
		if e != nil {
			return UploadPlan{}, e
		}
		p.Files = append(p.Files, UploadFile{Name: filepath.Base(path), Path: path, Size: info.Size()})
	}
	if p.Published, e = a.publishedVersion(ctx, host); e != nil {
		return UploadPlan{}, e
	}
	return p, nil
}

// UploadCreative puts a built version on the creative release: the package
// first, then update.json, then reads both back from the public addresses.
func (a *Admin) UploadCreative(ctx context.Context, version string, host ReleaseHost, log func(string)) (CreativeRelease, error) {
	r, manifest, e := a.creativeRecord(version)
	if e != nil {
		return r, e
	}
	log("检查 creative Release …")
	rel, e := host.Release(ctx)
	if e != nil {
		return r, e
	}
	if !rel.Prerelease {
		return r, errors.New("creative Release 现在不是 Pre-release。请先在网页上把它改回 Pre-release：否则 GitHub 可能把它当作 Latest，已交付的电商版旧程序会去读它")
	}
	if rel.Latest {
		return r, errors.New("creative Release 现在是 Latest。已交付的电商版旧程序每天读 Latest，请先在网页上取消，再上传")
	}
	published, e := a.publishedVersion(ctx, host)
	if e != nil {
		return r, e
	}
	if published != "" && update.Compare(version, published) < 0 {
		return r, fmt.Errorf("Release 上现在是 %s，比 %s 新。不能用旧版本替换新版本", published, version)
	}
	pkg := filepath.Base(r.UpdateZip)
	log("上传更新包 " + pkg + " …")
	if e = host.Upload(ctx, r.UpdateZip); e != nil {
		return r, fmt.Errorf("上传更新包失败，update.json 没有改动，顾客仍然看到原来的版本：%w", e)
	}
	log("上传 update.json …")
	if e = host.Upload(ctx, r.Manifest); e != nil {
		return r, fmt.Errorf("更新包已上传，但 update.json 上传失败，顾客仍然看到原来的版本，可以再点一次上传：%w", e)
	}
	log("从公开地址读回两个文件核对 …")
	if e = verifyPublished(ctx, host, "update.json", manifest); e != nil {
		return r, e
	}
	want, e := os.ReadFile(r.UpdateZip)
	if e != nil {
		return r, e
	}
	if e = verifyPublished(ctx, host, pkg, want); e != nil {
		return r, e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.d.Creative {
		if a.d.Creative[i].Version == version {
			a.d.Creative[i].UploadedAt = now()
			r = a.d.Creative[i]
		}
	}
	log("完成：顾客的软件下次检查更新时会看到 " + version + "。")
	return r, a.saveLocked()
}

// verifyPublished reads a file back from the public address and compares it.
// GitHub can take a moment to serve a replaced file, so it tries a few times.
func verifyPublished(ctx context.Context, host ReleaseHost, name string, want []byte) error {
	var got []byte
	var e error
	for i := 0; i < 5; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(publishRetry):
			}
		}
		got, e = host.Fetch(ctx, name)
		if e == nil && bytes.Equal(got, want) {
			return nil
		}
	}
	if e != nil {
		return fmt.Errorf("已上传，但从公开地址读不到 %s：%w。请到网页上看一下", name, e)
	}
	return fmt.Errorf("已上传，但从公开地址读回的 %s 和本机的不一致。请到网页上看一下", name)
}

// publishRetry is the wait between read-backs; tests shorten it.
var publishRetry = 3 * time.Second

// GitHubHost is the creative release through the GitHub CLI.
type GitHubHost struct{}

func ghPath() (string, error) {
	if p, e := exec.LookPath("gh"); e == nil {
		return p, nil
	}
	p := filepath.Join(os.Getenv("ProgramFiles"), "GitHub CLI", "gh.exe")
	if _, e := os.Stat(p); e == nil {
		return p, nil
	}
	return "", errors.New("没有找到 GitHub CLI（gh）。请先安装 GitHub CLI，在命令行运行 gh auth login 登录，再重新打开管理工具")
}

func runGH(ctx context.Context, args ...string) (string, error) {
	gh, e := ghPath()
	if e != nil {
		return "", e
	}
	cmd := exec.CommandContext(ctx, gh, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if e = cmd.Run(); e != nil {
		msg := strings.TrimSpace(errOut.String())
		if strings.Contains(msg, "gh auth login") || strings.Contains(msg, "authentication") {
			return "", errors.New("GitHub CLI 没有登录。请在命令行运行 gh auth login 登录后再试")
		}
		if msg == "" {
			msg = e.Error()
		}
		return "", errors.New(msg)
	}
	return out.String(), nil
}

func (GitHubHost) Release(ctx context.Context) (HostRelease, error) {
	out, e := runGH(ctx, "release", "view", CreativeTag, "--repo", CreativeRepo, "--json", "isPrerelease,isDraft,assets")
	if e != nil {
		if strings.Contains(e.Error(), "release not found") {
			return HostRelease{}, errors.New("仓库里没有标签为 creative 的 Release，请先按 docs/release-github.md 建好")
		}
		return HostRelease{}, fmt.Errorf("读取 creative Release 失败：%w", e)
	}
	var v struct {
		IsPrerelease bool `json:"isPrerelease"`
		IsDraft      bool `json:"isDraft"`
		Assets       []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if e = json.Unmarshal([]byte(out), &v); e != nil {
		return HostRelease{}, e
	}
	if v.IsDraft {
		return HostRelease{}, errors.New("creative Release 是草稿，顾客读不到。请先在网页上发布它（保持 Pre-release）")
	}
	r := HostRelease{Prerelease: v.IsPrerelease}
	for _, a := range v.Assets {
		r.Assets = append(r.Assets, a.Name)
	}
	// No Latest release at all answers 404, which gh reports as an error.
	latest, e := runGH(ctx, "api", "repos/"+CreativeRepo+"/releases/latest", "--jq", ".tag_name")
	r.Latest = e == nil && strings.TrimSpace(latest) == CreativeTag
	return r, nil
}

func (GitHubHost) Upload(ctx context.Context, path string) error {
	_, e := runGH(ctx, "release", "upload", CreativeTag, path, "--repo", CreativeRepo, "--clobber")
	return e
}

func (GitHubHost) Fetch(ctx context.Context, name string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	// A query string avoids a cached copy of a file that was just replaced.
	req, e := http.NewRequestWithContext(ctx, "GET", CreativeReleaseURL+name+"?t="+fmt.Sprint(time.Now().UnixNano()), nil)
	if e != nil {
		return nil, e
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, ErrNotPublished
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<30))
}
