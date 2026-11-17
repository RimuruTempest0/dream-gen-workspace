package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"imageworkbench/internal/license"
	"imageworkbench/internal/packcheck"
	"imageworkbench/internal/update"
)

// The hosted edition (托管版) of the mass-market line, admin 1.8.0: the same
// program as the open edition plus a signed customer.dat with the provider's
// lines (docs/adr/0003). Its package is put together from the newest version
// built on the "发布大众版" page and is never built on its own, so the program a
// hosted user holds is the one the line's update address serves.

// HostedSince is the first mass-market version whose program reads customer.dat.
// An older program would run as the open edition and ignore the licence.
const HostedSince = "0.7.34"

// versionLess orders versions like 0.7.9 < 0.7.34.
func versionLess(a, b string) bool {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(x) || i < len(y); i++ {
		var m, n int
		if i < len(x) {
			m, _ = strconv.Atoi(x[i])
		}
		if i < len(y) {
			n, _ = strconv.Atoi(y[i])
		}
		if m != n {
			return m < n
		}
	}
	return false
}

// HostedSource is what a hosted user's page shows before making a package.
type HostedSource struct {
	Version string `json:"version"`
	At      string `json:"at"` // when that version was built
	// Blocked is why no package can be made now, or "".
	Blocked string `json:"blocked"`
}

// hostedProgramLocked finds the program folder of the newest mass-market version
// built by this admin tool.
func (a *Admin) hostedProgramLocked() (CreativeRelease, string, error) {
	var newest CreativeRelease
	for _, r := range a.d.Creative {
		if newest.Version == "" || versionLess(newest.Version, r.Version) {
			newest = r
		}
	}
	if newest.Version == "" {
		return newest, "", errors.New("还没有构建过大众版。请先到“发布大众版”构建一版，托管版的包用它的程序组装")
	}
	if versionLess(newest.Version, HostedSince) {
		return newest, "", fmt.Errorf("最新构建的大众版是 %s，它的程序还不认授权文件。托管版需要 %s 或更高的版本，请先到“发布大众版”构建", newest.Version, HostedSince)
	}
	dir := filepath.Join(a.d.RepoRoot, "release", "creative", "build", newest.Version)
	info, e := update.ReadInfo(filepath.Join(dir, "version.json"))
	if e != nil {
		return newest, "", fmt.Errorf("找不到大众版 %s 构建出的程序（%s）。请到“发布大众版”重新构建这一版", newest.Version, dir)
	}
	if info.Line != CreativeLine || info.Version != newest.Version {
		return newest, "", fmt.Errorf("%s 里的程序不是大众版 %s，请到“发布大众版”重新构建这一版", dir, newest.Version)
	}
	return newest, dir, nil
}

// HostedSource reads which built version a hosted package would be made of.
func (a *Admin) HostedSource() HostedSource {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.d.RepoRoot == "" {
		return HostedSource{Blocked: "请先在“设置与签名密钥”中选择软件源码目录"}
	}
	r, _, e := a.hostedProgramLocked()
	s := HostedSource{Version: r.Version, At: r.At}
	if e != nil {
		s.Blocked = e.Error()
	}
	return s
}

// buildHosted puts the hosted package of a mass-market user together: the
// program files of version as built for the line, plus the user's customer.dat.
func (a *Admin) buildHosted(_ context.Context, customerID, version string, localDataDir string, log func(string)) (Delivery, error) {
	a.mu.Lock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		a.mu.Unlock()
		return Delivery{}, e
	}
	repo := a.d.RepoRoot
	softwareID, name := c.SoftwareID, c.Name
	profile, revision, e := a.profileLocked(customerID)
	secrets := a.customerSecretsLocked(c)
	var built CreativeRelease
	var program string
	if e == nil && repo == "" {
		e = errors.New("请先在“设置”中选择软件源码目录")
	}
	if e == nil {
		built, program, e = a.hostedProgramLocked()
	}
	a.mu.Unlock()
	if e != nil {
		return Delivery{}, e
	}
	if built.Version != version {
		return Delivery{}, fmt.Errorf("托管版的包用最新构建的大众版 %s 组装，不是 %s", built.Version, version)
	}
	log(fmt.Sprintf("组装托管版的包：大众版 %s 的程序（构建于 %s）加这位顾客的授权文件，不重新构建。", built.Version, built.At))
	out := filepath.Join(repo, "release", "customers", softwareID)
	if e = os.MkdirAll(out, 0o755); e != nil {
		return Delivery{}, e
	}
	zipPath := filepath.Join(out, zipName(name, version))
	log("打包 " + filepath.Base(zipPath) + " …")
	if e = writeZipWith(program, zipPath, programEntries, map[string][]byte{license.FileName: profile}); e != nil {
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
	if c, e = a.customerLocked(customerID); e != nil {
		return Delivery{}, e
	}
	c.Deliveries = append(c.Deliveries, d)
	c.VaultRevision, c.Changed = revision, false
	for _, id := range c.Slots {
		if k, e := a.keyLocked(id); e == nil {
			k.Delivered = true
		}
	}
	c.log("delivered", fmt.Sprintf("生成托管版交付包 %s（线路版本 %d）", filepath.Base(zipPath), revision))
	log("完成：" + zipPath)
	return d, a.saveLocked()
}
