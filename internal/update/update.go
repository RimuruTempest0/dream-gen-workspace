// Package update describes program updates: the signed manifest the provider
// publishes (update.json), checking an update package before it is used, and
// replacing the program files with a rollback if anything fails.
//
// The manifest is signed with the same Ed25519 key as customer.dat, so only the
// provider can announce an update, and it names the SHA-256 of the package.
package update

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Manifest formats. Format 1 is the e-commerce line: it names no product line
// and is the only format delivered e-commerce apps accept. Format 2 names its
// product line; an app built for a line accepts format 2 for that line only.
// Both are signed with the same key, so the format and line are what keeps one
// line from installing the other's program (docs/adr/0001).
const (
	formatPlain = 1
	formatLine  = 2
)

var linePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

func formatFor(line string) int {
	if line == "" {
		return formatPlain
	}
	return formatLine
}

// Manifest is what update.json announces.
type Manifest struct {
	Format      int    `json:"format"`
	Line        string `json:"line,omitempty"` // product line; "" = e-commerce (format 1)
	Version     string `json:"version"`
	MinVersion  string `json:"minVersion,omitempty"` // oldest version that may update in place
	PublishedAt string `json:"publishedAt"`
	Notes       string `json:"notes"`
	URL         string `json:"url"`    // the update package
	SHA256      string `json:"sha256"` // of the package
	Size        int64  `json:"size"`
}

type signedFile struct {
	Format    int    `json:"format"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

var ErrSignature = errors.New("更新清单无效或已被修改")

var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Sign produces update.json.
func Sign(m Manifest, priv ed25519.PrivateKey) ([]byte, error) {
	m.Format = formatFor(m.Line)
	if m.Line != "" && !linePattern.MatchString(m.Line) {
		return nil, errors.New("产品线名称无效")
	}
	if !versionPattern.MatchString(m.Version) || (m.MinVersion != "" && !versionPattern.MatchString(m.MinVersion)) {
		return nil, errors.New("版本号格式应为 0.7.13 这样的三段数字")
	}
	if !strings.HasPrefix(m.URL, "https://") && !strings.HasPrefix(m.URL, "http://") {
		return nil, errors.New("下载地址必须以 https:// 开头")
	}
	if len(m.SHA256) != 64 || m.Size <= 0 {
		return nil, errors.New("缺少更新包的 SHA-256 或大小")
	}
	if m.PublishedAt == "" {
		m.PublishedAt = time.Now().UTC().Format(time.RFC3339)
	}
	payload, e := json.Marshal(m)
	if e != nil {
		return nil, e
	}
	return json.MarshalIndent(signedFile{Format: m.Format, Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))}, "", "  ")
}

// Verify checks update.json against the trusted public keys (base64) and the
// product line of the program reading it ("" = e-commerce). A manifest of any
// other line or format is refused like a bad signature.
func Verify(data []byte, publicKeys []string, line string) (Manifest, error) {
	var f signedFile
	if json.Unmarshal(data, &f) != nil || f.Format != formatFor(line) {
		return Manifest{}, ErrSignature
	}
	payload, e1 := base64.StdEncoding.DecodeString(f.Payload)
	sig, e2 := base64.StdEncoding.DecodeString(f.Signature)
	if e1 != nil || e2 != nil {
		return Manifest{}, ErrSignature
	}
	trusted := false
	for _, k := range publicKeys {
		pub, e := base64.StdEncoding.DecodeString(strings.TrimSpace(k))
		if e == nil && len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, payload, sig) {
			trusted = true
			break
		}
	}
	if !trusted {
		return Manifest{}, ErrSignature
	}
	var m Manifest
	if json.Unmarshal(payload, &m) != nil || m.Format != f.Format || m.Line != line || !versionPattern.MatchString(m.Version) || len(m.SHA256) != 64 || m.Size <= 0 {
		return Manifest{}, ErrSignature
	}
	return m, nil
}

// Compare compares two x.y.z versions (-1, 0, 1); malformed ones compare lowest.
func Compare(a, b string) int {
	pa, pb := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		switch {
		case pa[i] < pb[i]:
			return -1
		case pa[i] > pb[i]:
			return 1
		}
	}
	return 0
}

func parse(v string) [3]int {
	out := [3]int{-1, -1, -1}
	if !versionPattern.MatchString(v) {
		return out
	}
	for i, p := range strings.Split(v, ".") {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

// Applies reports whether m is an update this version can take in place.
func (m Manifest) Applies(current string) (bool, string) {
	if Compare(m.Version, current) <= 0 {
		return false, ""
	}
	if m.MinVersion != "" && Compare(current, m.MinVersion) < 0 {
		return false, fmt.Sprintf("新版本 %s 不能从当前版本直接更新（需要 %s 以上），请向软件提供方获取完整包", m.Version, m.MinVersion)
	}
	return true, ""
}

// Package entries. Everything else (data, data-location.txt, customer.dat,
// cache) is never part of an update and never touched.
var topFiles = map[string]bool{"ImageWorkbench.exe": true, "product-browser.exe": true, "使用说明.txt": true, "version.json": true}
var topDirs = map[string]bool{"runtime": true, "agent": true}

// AllowedEntry reports whether a package path may be written into the program folder.
func AllowedEntry(name string) bool {
	name = strings.TrimPrefix(strings.ReplaceAll(name, `\`, "/"), "./")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "." {
			return false
		}
	}
	top, rest, nested := strings.Cut(name, "/")
	if !nested {
		return topFiles[top]
	}
	return topDirs[top] && rest != ""
}

// FileSHA256 hashes a file.
func FileSHA256(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Extract unpacks a checked update package into dir (which must not exist).
// The package must hold the program and version.json and nothing outside the
// allowed entries.
func Extract(pkg, dir string) error {
	z, e := zip.OpenReader(pkg)
	if e != nil {
		return errors.New("更新包无法打开")
	}
	defer z.Close()
	if len(z.File) > 5000 {
		return errors.New("更新包内容异常")
	}
	seen := map[string]bool{}
	var total uint64
	for _, f := range z.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if f.FileInfo().IsDir() {
			continue
		}
		if !AllowedEntry(name) || f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("更新包包含不允许的文件：%s", name)
		}
		total += f.UncompressedSize64
		if total > 2<<30 {
			return errors.New("更新包过大")
		}
		seen[name] = true
	}
	if !seen["ImageWorkbench.exe"] || !seen["version.json"] {
		return errors.New("更新包缺少程序或版本信息")
	}
	if e = os.MkdirAll(dir, 0o700); e != nil {
		return e
	}
	for _, f := range z.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if f.FileInfo().IsDir() {
			continue
		}
		dest := filepath.Join(dir, filepath.FromSlash(name))
		if e = os.MkdirAll(filepath.Dir(dest), 0o700); e != nil {
			return e
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		w, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
		if e == nil {
			_, e = io.Copy(w, io.LimitReader(r, int64(f.UncompressedSize64)+1))
			if closeErr := w.Close(); e == nil {
				e = closeErr
			}
		}
		r.Close()
		if e != nil {
			return e
		}
	}
	return nil
}

// Info is the part of version.json an update is checked against.
type Info struct {
	Version       string `json:"version"`
	RuntimeSHA256 string `json:"runtimeSHA256"`
	ClaudeSHA256  string `json:"claudeSHA256"`
	Line          string `json:"productLine,omitempty"`
}

func ReadInfo(path string) (Info, error) {
	var v Info
	b, e := os.ReadFile(path)
	if e != nil {
		return v, e
	}
	return v, json.Unmarshal(TrimBOM(b), &v)
}

// TrimBOM drops a UTF-8 byte order mark; build.ps1 writes version.json with one.
func TrimBOM(b []byte) []byte { return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")) }

// CheckComponents makes sure an update without runtime/ or agent/ fits the
// components already in the program folder; otherwise the full package is needed.
func CheckComponents(stage, program string) error {
	want, e := ReadInfo(filepath.Join(stage, "version.json"))
	if e != nil {
		return errors.New("更新包的版本信息无法读取")
	}
	check := func(dir, file, sum, label string) error {
		if _, e := os.Stat(filepath.Join(stage, dir)); e == nil || sum == "" {
			return nil // the package brings it, or the version does not pin it
		}
		have, e := FileSHA256(filepath.Join(program, dir, file))
		if e != nil || !strings.EqualFold(have, sum) {
			return fmt.Errorf("这个版本换了新的%s，不能一键更新，请向软件提供方获取完整包", label)
		}
		return nil
	}
	if e = check("runtime", "msedgewebview2.exe", want.RuntimeSHA256, "浏览器运行组件"); e != nil {
		return e
	}
	return check("agent", filepath.Join("claude-code", "claude.exe"), want.ClaudeSHA256, "内置 Claude Code")
}

// Apply moves the staged files into the program folder. Every replaced entry is
// first moved into backup; if any step fails, everything is put back and the
// error returned. Renames are retried for a while because files of the old
// program (its browser processes) may still be closing.
func Apply(stage, program, backup string) error {
	entries, e := os.ReadDir(stage)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(backup, 0o700); e != nil {
		return e
	}
	type step struct {
		name   string
		hadOld bool
	}
	done := []step{}
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			target := filepath.Join(program, done[i].name)
			_ = os.RemoveAll(target)
			if done[i].hadOld {
				_ = rename(filepath.Join(backup, done[i].name), target)
			}
		}
	}
	for _, d := range entries {
		name := d.Name()
		if (d.IsDir() && !topDirs[name]) || (!d.IsDir() && !topFiles[name]) {
			continue
		}
		target := filepath.Join(program, name)
		s := step{name: name}
		if _, e = os.Lstat(target); e == nil {
			if e = rename(target, filepath.Join(backup, name)); e != nil {
				rollback()
				return fmt.Errorf("无法替换 %s（%w），已恢复原文件", name, e)
			}
			s.hadOld = true
		}
		done = append(done, s)
		if e = rename(filepath.Join(stage, name), target); e != nil {
			rollback()
			return fmt.Errorf("无法写入 %s（%w），已恢复原文件", name, e)
		}
	}
	return nil
}

// RenameWait is how long Apply keeps retrying a rename; tests shorten it.
var RenameWait = 30 * time.Second

func rename(from, to string) error {
	deadline := time.Now().Add(RenameWait)
	for {
		e := os.Rename(from, to)
		if e == nil || time.Now().After(deadline) {
			return e
		}
		time.Sleep(500 * time.Millisecond)
	}
}
