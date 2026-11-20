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
	"imageworkbench/internal/update"
)

// Builder makes the program files of a package (scripts/build.ps1 in practice).
type Builder interface {
	Build(ctx context.Context, b BuildRequest, log func(string)) error
}

type BuildRequest struct {
	// RepoRoot is the source folder: what is built lands in its release\ folder.
	// BuildDir is the product line's build folder, where the build script of the
	// commit being built runs (its release\ is a link to RepoRoot's).
	RepoRoot   string
	BuildDir   string
	Version    string
	ReleaseDir string // relative to RepoRoot, under release/
	ZipPath    string // relative to RepoRoot; the build's own (temporary) ZIP
	UpdateURL  string // update.json address built into the app; "" = none
	// ProductLine and UpdateKey make a build of the mass-market line: updates
	// and licence files are checked with UpdateKey (ADR 0001, 0003).
	ProductLine string
	UpdateKey   string
}

// Program entries of a package, as in scripts/build.ps1.
var programEntries = []string{"ImageWorkbench.exe", "product-browser.exe", "runtime", "agent", "使用说明.txt", "version.json"}

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
			return nil, 0, fmt.Errorf("%s 还没有通过能力验证；托管版的智能生图需要已验证的 LLM 线路", SlotName(slot))
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
		// LLM lines carry their output limit explicitly (default 65536), so a customer
		// package never falls back to an older client default.
		maxOutput := u.MaxOutputTokens
		if u.Kind == "llm" {
			maxOutput = core.LLMOutputLimit(maxOutput)
		}
		conns = append(conns, license.Connection{Slot: slot, Kind: u.Kind, Name: SlotName(slot), BaseURL: u.BaseURL, Model: u.Model, Mode: u.Mode, AuthMode: u.AuthMode, MaxOutputTokens: maxOutput, UsageURL: usage, Verified: k.Verified, KeyObf: obf, Protocol: u.Protocol})
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
	data, e := license.Sign(license.Profile{SoftwareID: c.SoftwareID, Customer: c.Name, IssuedAt: now(), TrialImages: c.TrialImages, Connections: conns, ImageBackups: backups, LinesRevision: revision, CodeCheck: check, ExpiresAt: c.ExpiresAt, Skills: c.Skills}, priv)
	return data, revision, e
}

// SetCustomerSkills chooses the built-in skills a customer's package comes with;
// choosing all of them is stored as "all". It takes effect with the next package
// or renewal file; the others start switched off in the app.
func (a *Admin) SetCustomerSkills(customerID string, ids []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return e
	}
	all := core.BuiltinSkills()
	known := map[string]bool{}
	chosen := []string{}
	for _, b := range all {
		known[b.ID] = true
		if slices.Contains(ids, b.ID) {
			chosen = append(chosen, b.ID)
		}
	}
	for _, id := range ids {
		if !known[id] {
			return fmt.Errorf("没有这个内置技能：%s", id)
		}
	}
	if len(chosen) == len(all) {
		chosen = nil
	}
	if slices.Equal(chosen, c.Skills) && (chosen == nil) == (c.Skills == nil) {
		return nil
	}
	c.Skills = chosen
	if chosen == nil {
		c.log("note", "内置技能改为全部")
	} else {
		c.log("note", fmt.Sprintf("内置技能改为 %d 个：%s", len(chosen), strings.Join(chosen, "、")))
	}
	return a.saveLocked()
}

// SetCustomerExpiry sets when the customer's licence ends (RFC 3339; "" = never).
// It takes effect with the next package or renewal file.
func (a *Admin) SetCustomerExpiry(customerID, expiresAt string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return e
	}
	expiresAt = strings.TrimSpace(expiresAt)
	if expiresAt != "" {
		at, e := time.Parse(time.RFC3339, expiresAt)
		if e != nil {
			return errors.New("有效期格式无效")
		}
		expiresAt = at.UTC().Format(time.RFC3339)
	}
	if expiresAt == c.ExpiresAt {
		return nil
	}
	c.ExpiresAt = expiresAt
	if expiresAt == "" {
		c.log("note", "有效期改为不限")
	} else {
		c.log("note", "有效期改为 "+expiresAt)
	}
	return a.saveLocked()
}

// WriteCustomerFile signs a new customer.dat for a customer who already has the
// program (renewal, new expiry or changed keys) and writes it to dest. Only this
// small file needs to reach the customer, who imports it in the app (0.7.12+).
func (a *Admin) WriteCustomerFile(customerID, dest string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return e
	}
	// A hosted user may already have the program (the open edition): the
	// licence file alone makes it the hosted edition.
	if c.Line != CreativeLine {
		return errRetiredLine
	}
	data, revision, e := a.profileLocked(customerID)
	if e != nil {
		return e
	}
	for _, s := range a.customerSecretsLocked(c) {
		if s.Value != "" && strings.Contains(string(data), s.Value) {
			return fmt.Errorf("授权文件里出现了%s明文，已停止", s.Kind)
		}
	}
	if e = os.WriteFile(dest, data, 0o644); e != nil {
		return e
	}
	c.VaultRevision, c.Changed = revision, false
	for _, id := range c.Slots {
		if k, e := a.keyLocked(id); e == nil {
			k.Delivered = true
		}
	}
	text := fmt.Sprintf("生成续费授权文件 %s（线路版本 %d", filepath.Base(dest), revision)
	if c.ExpiresAt != "" {
		text += "，有效期至 " + c.ExpiresAt
	}
	c.log("renewed", text+"）")
	return a.saveLocked()
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

// BuildPackage makes the package of a hosted user of the mass-market line: the
// newest built program plus their signed customer.dat (see buildHosted). The
// e-commerce line, which built a program per customer, was retired in admin
// 1.12.0.
func (a *Admin) BuildPackage(ctx context.Context, customerID, version string, localDataDir string, log func(string)) (Delivery, error) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
		return Delivery{}, errors.New("版本号格式应为 0.6.2 这样的三段数字")
	}
	a.mu.Lock()
	c, e := a.customerLocked(customerID)
	if e == nil && c.Line != CreativeLine {
		e = errRetiredLine
	}
	a.mu.Unlock()
	if e != nil {
		return Delivery{}, e
	}
	return a.buildHosted(ctx, customerID, version, localDataDir, log)
}

// errRetiredLine refuses a ledger record of the retired e-commerce line.
var errRetiredLine = errors.New("这条记录属于已停用的电商版，不能再生成交付包或授权文件")

// writeZipOf zips the named entries of a built program folder.
func writeZipOf(dir, path string, entries []string) error {
	return writeZipWith(dir, path, entries, nil)
}

// writeZipWith also adds files that are not in the folder (name → content).
// The 《使用说明》 in other languages are added when the build has them.
func writeZipWith(dir, path string, entries []string, extra map[string][]byte) error {
	entries = slices.Clone(entries)
	for _, m := range update.Manuals {
		if _, e := os.Stat(filepath.Join(dir, m)); e == nil && !slices.Contains(entries, m) {
			entries = append(entries, m)
		}
	}
	tmp := path + ".partial"
	f, e := os.Create(tmp)
	if e != nil {
		return e
	}
	defer os.Remove(tmp)
	zw := zip.NewWriter(f)
	for _, entry := range entries {
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
	for name, body := range extra {
		w, e := zw.Create(name)
		if e == nil {
			_, e = w.Write(body)
		}
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

// ScriptBuilder runs scripts/build.ps1 of the build folder for a clean build
// that is not linked to the developer's own data.
type ScriptBuilder struct{}

func (ScriptBuilder) Build(ctx context.Context, b BuildRequest, log func(string)) error {
	args := []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(b.BuildDir, "scripts", "build.ps1"), "-Version", b.Version, "-NoLocalProfile", "-ReleaseDirectory", b.ReleaseDir, "-ZipPath", b.ZipPath}
	args = append(args, "-ProductLine", b.ProductLine, "-UpdatePublicKey", b.UpdateKey)
	if b.UpdateURL != "" {
		args = append(args, "-UpdateURL", b.UpdateURL)
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", args...)
	cmd.Dir = b.BuildDir
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
