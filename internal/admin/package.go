package admin

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"imageworkbench/internal/core"
	"imageworkbench/internal/license"
	"imageworkbench/internal/packcheck"
)

// Builder makes the program files of a package (scripts/build.ps1 in practice).
type Builder interface {
	Build(ctx context.Context, b BuildRequest, log func(string)) error
}

type BuildRequest struct {
	RepoRoot   string
	Version    string
	ReleaseDir string // relative to RepoRoot, under release/
	ZipPath    string // relative to RepoRoot; the build's own (temporary) ZIP
	PublicKey  string
}

// Shipping entries of a package, as in scripts/build.ps1, plus customer.dat.
var shippingEntries = []string{"ImageWorkbench.exe", "product-browser.exe", "runtime", "agent", "使用说明.txt", "version.json", license.FileName}

// CustomerProfile assembles and signs customer.dat for a customer: image line 1,
// LLM line 1 and whichever optional lines have a key (keys obfuscated; each
// key's upstream quota is the real guard) and the check value of the
// activation code.
func (a *Admin) CustomerProfile(customerID string) ([]byte, int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.profileLocked(customerID)
}

func (a *Admin) profileLocked(customerID string) ([]byte, int, error) {
	c, e := a.customerLocked(customerID)
	if e != nil {
		return nil, 0, e
	}
	priv, e := a.signingKeyLocked()
	if e != nil {
		return nil, 0, e
	}
	codeBytes, e := a.secret(c.CodeRef)
	if e != nil {
		return nil, 0, e
	}
	conns := []license.Connection{}
	for _, slot := range AllSlots {
		if c.Slots[slot] == "" && !slices.Contains(RequiredSlots, slot) {
			continue
		}
		k, e := a.keyLocked(c.Slots[slot])
		if e != nil || k.Status != "assigned" || k.CustomerID != c.ID {
			return nil, 0, fmt.Errorf("%s 还没有分配 Key", SlotName(slot))
		}
		u, e := a.upstreamLocked(k.UpstreamID)
		if e != nil {
			return nil, 0, e
		}
		if u.Kind == "llm" && !k.Verified {
			return nil, 0, fmt.Errorf("%s 还没有通过能力验证；客户版的智能生图需要已验证的 LLM 线路", SlotName(slot))
		}
		key, e := a.secret(k.SecretRef)
		if e != nil {
			return nil, 0, e
		}
		obf, e := license.ObfuscateKey(string(key), c.SoftwareID)
		if e != nil {
			return nil, 0, e
		}
		usage := ""
		if u.PerKeyQuota {
			usage = u.UsageURL
		}
		// LLM lines carry their output limit explicitly (default 32768), so a customer
		// package never falls back to an older client default.
		maxOutput := u.MaxOutputTokens
		if u.Kind == "llm" {
			maxOutput = core.LLMOutputLimit(maxOutput)
		}
		conns = append(conns, license.Connection{Slot: slot, Kind: u.Kind, Name: SlotName(slot), BaseURL: u.BaseURL, Model: u.Model, Mode: u.Mode, AuthMode: u.AuthMode, MaxOutputTokens: maxOutput, UsageURL: usage, Verified: k.Verified, KeyObf: obf})
	}
	// Only assigned backup lines are switched to; the saved order keeps the rest.
	backups, order := []string{}, c.ImageBackups
	if len(order) == 0 {
		order = defaultOrder
	}
	for _, slot := range order {
		if c.Slots[slot] != "" {
			backups = append(backups, slot)
		}
	}
	if len(backups) == 0 {
		return nil, 0, errors.New("至少还要分配一条备用图片线路（图片线路 2–6 任选一条），图片线路 1 不可用时才有线路可换")
	}
	if e = license.CheckLines(conns, backups); e != nil {
		return nil, 0, e
	}
	check, e := license.NewCodeCheck(string(codeBytes), c.SoftwareID)
	if e != nil {
		return nil, 0, e
	}
	revision := c.VaultRevision
	if c.Changed || revision == 0 {
		revision++
	}
	data, e := license.Sign(license.Profile{SoftwareID: c.SoftwareID, Customer: c.Name, IssuedAt: now(), TrialImages: c.TrialImages, Connections: conns, ImageBackups: backups, LinesRevision: revision, CodeCheck: check}, priv)
	return data, revision, e
}

// customerSecretsLocked lists every key of the customer and its code; none may
// appear in a package in plain text.
func (a *Admin) customerSecretsLocked(c *Customer) []packcheck.Secret {
	out := []packcheck.Secret{}
	for slot, id := range c.Slots {
		if k, e := a.keyLocked(id); e == nil {
			if v, e := a.secret(k.SecretRef); e == nil {
				out = append(out, packcheck.Secret{Kind: SlotName(slot) + " Key", Value: string(v)})
			}
		}
	}
	if code, e := a.secret(c.CodeRef); e == nil {
		out = append(out, packcheck.Secret{Kind: "激活码", Value: string(code)}, packcheck.Secret{Kind: "激活码", Value: strings.ReplaceAll(string(code), "-", "")})
	}
	return out
}

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

func zipName(customer, version string) string {
	name := strings.TrimSpace(unsafeName.ReplaceAllString(customer, "_"))
	return fmt.Sprintf("ImageWorkbench-%s-%s.zip", name, version)
}

// BuildPackage builds the program, adds the signed customer.dat, zips it as
// ImageWorkbench-<客户>-<版本>.zip and checks the ZIP before recording delivery.
func (a *Admin) BuildPackage(ctx context.Context, customerID, version string, builder Builder, localDataDir string, log func(string)) (Delivery, error) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
		return Delivery{}, errors.New("版本号格式应为 0.6.2 这样的三段数字")
	}
	a.mu.Lock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		a.mu.Unlock()
		return Delivery{}, e
	}
	repo := a.d.RepoRoot
	pub := a.d.SigningPublicKey
	softwareID, name := c.SoftwareID, c.Name
	profile, revision, e := a.profileLocked(customerID)
	secrets := a.customerSecretsLocked(c)
	a.mu.Unlock()
	if e != nil {
		return Delivery{}, e
	}
	if repo == "" {
		return Delivery{}, errors.New("请先在“设置”中选择软件源码目录")
	}
	rel := filepath.Join("release", "customers", softwareID, version)
	releaseDir := filepath.Join(repo, rel)
	baseZip := filepath.Join(rel, "build-check.zip")
	log("开始构建程序文件（约 3–5 分钟）…")
	if e = builder.Build(ctx, BuildRequest{RepoRoot: repo, Version: version, ReleaseDir: rel, ZipPath: baseZip, PublicKey: pub}, log); e != nil {
		return Delivery{}, fmt.Errorf("构建失败：%w", e)
	}
	os.Remove(filepath.Join(repo, baseZip))
	if e = os.WriteFile(filepath.Join(releaseDir, license.FileName), profile, 0o644); e != nil {
		return Delivery{}, e
	}
	zipPath := filepath.Join(repo, "release", "customers", softwareID, zipName(name, version))
	log("打包 " + filepath.Base(zipPath) + " …")
	if e = writeZip(releaseDir, zipPath); e != nil {
		return Delivery{}, e
	}
	log("检查交付包中没有明文 Key 和本机连接 …")
	findings, e := packcheck.Check(zipPath, packcheck.Options{DataDir: localDataDir, CustomerFile: true, Secrets: secrets})
	if e == nil && len(findings) > 0 {
		e = errors.New(strings.Join(findings, "；"))
	}
	if e != nil {
		os.Remove(zipPath)
		return Delivery{}, fmt.Errorf("交付包检查未通过，已删除：%w", e)
	}
	sum, e := fileSHA256(zipPath)
	if e != nil {
		return Delivery{}, e
	}
	d := Delivery{Version: version, At: now(), Zip: zipPath, SHA256: sum, VaultRevision: revision}
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e = a.customerLocked(customerID)
	if e != nil {
		return Delivery{}, e
	}
	c.Deliveries = append(c.Deliveries, d)
	c.VaultRevision, c.Changed = revision, false
	for _, id := range c.Slots {
		if k, e := a.keyLocked(id); e == nil {
			k.Delivered = true
		}
	}
	c.log("delivered", fmt.Sprintf("生成交付包 %s（线路版本 %d）", filepath.Base(zipPath), revision))
	log("完成：" + zipPath)
	return d, a.saveLocked()
}

func writeZip(dir, path string) error {
	tmp := path + ".partial"
	f, e := os.Create(tmp)
	if e != nil {
		return e
	}
	defer os.Remove(tmp)
	zw := zip.NewWriter(f)
	for _, entry := range shippingEntries {
		root := filepath.Join(dir, entry)
		if _, e := os.Stat(root); e != nil {
			zw.Close()
			f.Close()
			return fmt.Errorf("构建结果缺少 %s", entry)
		}
		e = filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
			if e != nil || d.IsDir() {
				return e
			}
			rel, _ := filepath.Rel(dir, p)
			w, e := zw.Create(filepath.ToSlash(rel))
			if e != nil {
				return e
			}
			src, e := os.Open(p)
			if e != nil {
				return e
			}
			defer src.Close()
			_, e = io.Copy(w, src)
			return e
		})
		if e != nil {
			zw.Close()
			f.Close()
			return e
		}
	}
	if e = zw.Close(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}

func fileSHA256(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))), nil
}

// ScriptBuilder runs scripts/build.ps1 for a clean, unlinked customer build.
type ScriptBuilder struct{}

func (ScriptBuilder) Build(ctx context.Context, b BuildRequest, log func(string)) error {
	args := []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(b.RepoRoot, "scripts", "build.ps1"), "-Version", b.Version, "-NoLocalProfile", "-ReleaseDirectory", b.ReleaseDir, "-LicensePublicKey", b.PublicKey, "-ZipPath", b.ZipPath}
	cmd := exec.CommandContext(ctx, "powershell.exe", args...)
	cmd.Dir = b.RepoRoot
	cmd.Env = append(os.Environ(), "PATH="+buildPath(b.RepoRoot))
	out, e := cmd.CombinedOutput()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l := strings.TrimSpace(line); l != "" {
			log(l)
		}
	}
	return e
}

// buildPath adds the bundled Node runtime that scripts/build.ps1 needs when
// Node is not on PATH (see .tools/node.ps1).
func buildPath(repo string) string {
	path := os.Getenv("PATH")
	if b, e := os.ReadFile(filepath.Join(repo, ".tools", "node.ps1")); e == nil {
		if m := regexp.MustCompile(`'([^']+node\.exe)'`).FindSubmatch(b); m != nil {
			path = filepath.Dir(string(m[1])) + ";" + path
		}
	}
	return path
}

// LocalDataDir finds the developer's own data directory (.local/profile-source.txt
// → that program's data-location.txt or data), whose lines must never ship.
func LocalDataDir(repo string) string {
	src, e := os.ReadFile(filepath.Join(repo, ".local", "profile-source.txt"))
	if e != nil {
		return ""
	}
	app := strings.TrimSpace(string(src))
	dir := filepath.Join(app, "data")
	if b, e := os.ReadFile(filepath.Join(app, "data-location.txt")); e == nil && strings.TrimSpace(string(b)) != "" {
		dir = strings.TrimSpace(string(b))
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(app, dir)
		}
	}
	if _, e := os.Stat(filepath.Join(dir, "app.db")); e != nil {
		return ""
	}
	return dir
}

// SetRepoRoot records the ImageWorkbench source folder used for building.
func (a *Admin) SetRepoRoot(dir string) error {
	if _, e := os.Stat(filepath.Join(dir, "scripts", "build.ps1")); e != nil {
		return errors.New("这个文件夹里没有 scripts/build.ps1，请选择 ImageWorkbench 源码目录")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.d.RepoRoot = dir
	return a.saveLocked()
}

// VerifyLLM runs the app's own capability check (a few short, billable LLM
// calls) on a key in a throw-away data folder, and marks the key verified.
func (a *Admin) VerifyLLM(keyID string) (core.LLMCapabilities, error) {
	a.mu.Lock()
	k, e := a.keyLocked(keyID)
	var u *Upstream
	var key []byte
	if e == nil {
		u, e = a.upstreamLocked(k.UpstreamID)
	}
	if e == nil && k.Kind != "llm" {
		e = errors.New("只有 LLM Key 需要能力验证")
	}
	if e == nil {
		key, e = a.secret(k.SecretRef)
	}
	upstream := Upstream{}
	if u != nil {
		upstream = *u
	}
	a.mu.Unlock()
	if e != nil {
		return core.LLMCapabilities{}, e
	}
	tmp, e := os.MkdirTemp("", "iw-admin-verify-")
	if e != nil {
		return core.LLMCapabilities{}, e
	}
	defer os.RemoveAll(tmp)
	s, e := core.NewService(filepath.Join(tmp, "data"))
	if e != nil {
		return core.LLMCapabilities{}, e
	}
	defer s.Dispose()
	c, e := s.SaveConnection(core.ConnectionInput{Kind: "llm", Name: upstream.Name, BaseURL: upstream.BaseURL, Model: upstream.Model, Key: string(key), Mode: upstream.Mode, AuthMode: upstream.AuthMode, MaxOutputTokens: upstream.MaxOutputTokens})
	if e != nil {
		return core.LLMCapabilities{}, e
	}
	caps, e := s.ValidateLLMCapabilities(c.ID, newID())
	if e != nil {
		return caps, e
	}
	ok := caps.Text && caps.Vision && caps.Tools && caps.Streaming
	a.mu.Lock()
	defer a.mu.Unlock()
	if k, e = a.keyLocked(keyID); e != nil {
		return caps, e
	}
	k.Verified, k.VerifiedAt = ok, time.Now().UTC().Format(time.RFC3339)
	if k.CustomerID != "" {
		if c, e := a.customerLocked(k.CustomerID); e == nil {
			c.Changed = true
		}
	}
	if e = a.saveLocked(); e != nil {
		return caps, e
	}
	if !ok {
		return caps, errors.New("能力验证未全部通过：" + strings.Join(caps.Details, "；"))
	}
	return caps, nil
}
