package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"imageworkbench/internal/update"
)

// In-app updates: the provider publishes a signed update.json and a small update
// package (the program without runtime and agent unless those changed). The app
// reads the manifest at start and every 24 hours (read-only, can be turned off),
// tells the user, and on request downloads the package, checks its SHA-256 and
// contents, and hands over to the new program, which waits for this one to exit,
// replaces the program files with a backup and restarts. data, data-location.txt
// and customer.dat are never part of an update.

// updateManifestURL is set at build time (scripts/build.ps1 -UpdateURL); empty
// builds never check for updates.
var updateManifestURL = ""

const (
	updateCheckEvery   = 24 * time.Hour
	updateFirstCheck   = time.Minute
	updatePrefKey      = "update-check"
	updateDir          = ".update"
	maxManifestBytes   = 1 << 20
	maxUpdatePackage   = 1 << 30
	updateFlag         = "--apply-update"
	updateCheckTimeout = 20 * time.Second
)

type UpdateStatus struct {
	Supported  bool   `json:"supported"` // this build knows where to look
	AutoCheck  bool   `json:"autoCheck"`
	Current    string `json:"current"`
	CheckedAt  string `json:"checkedAt,omitempty"`
	Checking   bool   `json:"checking,omitempty"`
	Available  bool   `json:"available"`
	Version    string `json:"version,omitempty"`
	Notes      string `json:"notes,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Stage      string `json:"stage,omitempty"` // downloading, ready
	Downloaded int64  `json:"downloaded,omitempty"`
	Error      string `json:"error,omitempty"`
}

type updateState struct {
	status   UpdateStatus
	manifest update.Manifest
	staged   string // folder with the checked new files
}

type updatePrefs struct {
	Off bool `json:"off"`
}

func (s *Service) updateKeysLocked() []string {
	if licensePublicKey == "" {
		return nil
	}
	return []string{licensePublicKey}
}

func (s *Service) updateSupportedLocked() bool {
	return updateManifestURL != "" && licensePublicKey != "" && s.programDir != ""
}

// updateBlockedLocked explains why no update is offered with this licence, or "".
func (s *Service) updateBlockedLocked() string {
	if s.lic.mode == "invalid" || s.lic.mode == "mismatch" {
		return "软件授权无效，不能在线更新。请先在“系统 → 连接 → 授权”导入授权文件。"
	}
	return ""
}

func (s *Service) UpdateStatus() UpdateStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.upd.status
	st.Supported, st.Current = s.updateSupportedLocked(), AppVersion
	var p updatePrefs
	_ = s.store.Get("preferences", updatePrefKey, &p)
	st.AutoCheck = !p.Off
	return st
}

func (s *Service) SetUpdateAutoCheck(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.Put("preferences", updatePrefKey, updatePrefs{Off: !on})
}

// watchUpdates checks shortly after start and then every 24 hours while allowed.
func (s *Service) watchUpdates() {
	timer := time.NewTimer(updateFirstCheck)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
		}
		s.mu.Lock()
		var p updatePrefs
		_ = s.store.Get("preferences", updatePrefKey, &p)
		run := s.updateSupportedLocked() && !p.Off && s.updateBlockedLocked() == ""
		s.mu.Unlock()
		if run {
			_, _ = s.CheckForUpdate()
		}
		timer.Reset(updateCheckEvery)
	}
}

// CheckForUpdate reads the manifest (no cost, nothing sent but the request).
func (s *Service) CheckForUpdate() (UpdateStatus, error) {
	s.mu.Lock()
	if !s.updateSupportedLocked() {
		s.mu.Unlock()
		return s.UpdateStatus(), errors.New("此版本没有设置更新地址，请向软件提供方获取完整包升级")
	}
	if blocked := s.updateBlockedLocked(); blocked != "" {
		s.mu.Unlock()
		return s.UpdateStatus(), errors.New(blocked)
	}
	if s.upd.status.Checking || s.upd.status.Stage == "downloading" {
		s.mu.Unlock()
		return s.UpdateStatus(), nil
	}
	s.upd.status.Checking = true
	keys, url := s.updateKeysLocked(), updateManifestURL
	s.mu.Unlock()
	m, e := fetchManifest(s.ctx, url, keys)
	s.mu.Lock()
	s.upd.status.Checking = false
	s.upd.status.CheckedAt = Now()
	if e != nil {
		s.upd.status.Error = e.Error()
		_ = s.store.appendLog("warn", "app", "", "检查更新失败："+e.Error())
	} else {
		ok, why := m.Applies(AppVersion)
		s.upd.manifest = m
		s.upd.status.Error = why
		s.upd.status.Available = ok
		if ok && s.upd.status.Version != m.Version {
			s.upd.status.Stage, s.upd.staged = "", ""
			_ = s.store.appendLog("info", "app", "", "发现新版本 "+m.Version)
		}
		s.upd.status.Version, s.upd.status.Notes, s.upd.status.Size = m.Version, m.Notes, m.Size
		if !ok {
			s.upd.status.Version, s.upd.status.Notes = "", ""
		}
	}
	s.mu.Unlock()
	s.signal()
	return s.UpdateStatus(), e
}

func fetchManifest(ctx context.Context, url string, keys []string) (update.Manifest, error) {
	ctx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return update.Manifest{}, e
	}
	resp, e := updateClient().Do(req)
	if e != nil {
		return update.Manifest{}, fmt.Errorf("无法连接更新服务器：%w", e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return update.Manifest{}, fmt.Errorf("更新服务器返回 HTTP %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes))
	if e != nil {
		return update.Manifest{}, e
	}
	return update.Verify(b, keys)
}

// DownloadUpdate fetches and checks the package announced by the last check.
// The result is staged next to the program; nothing is replaced yet.
func (s *Service) DownloadUpdate() (UpdateStatus, error) {
	s.mu.Lock()
	m, dir := s.upd.manifest, s.programDir
	if blocked := s.updateBlockedLocked(); blocked != "" {
		s.mu.Unlock()
		return s.UpdateStatus(), errors.New(blocked)
	}
	if !s.upd.status.Available || dir == "" {
		s.mu.Unlock()
		return s.UpdateStatus(), errors.New("没有可下载的新版本，请先检查更新")
	}
	if s.upd.status.Stage == "downloading" {
		s.mu.Unlock()
		return s.UpdateStatus(), nil
	}
	s.upd.status.Stage, s.upd.status.Downloaded, s.upd.status.Error = "downloading", 0, ""
	s.mu.Unlock()
	s.signal()
	staged, e := s.fetchAndStage(m, dir)
	s.mu.Lock()
	if e != nil {
		s.upd.status.Stage, s.upd.status.Error = "", e.Error()
		_ = s.store.appendLog("error", "app", "", "下载更新失败："+e.Error())
	} else {
		s.upd.status.Stage, s.upd.staged = "ready", staged
		_ = s.store.appendLog("info", "app", "", "新版本 "+m.Version+" 已下载并通过校验")
	}
	s.mu.Unlock()
	s.signal()
	return s.UpdateStatus(), e
}

func (s *Service) fetchAndStage(m update.Manifest, program string) (string, error) {
	if m.Size > maxUpdatePackage {
		return "", errors.New("更新包过大")
	}
	work := filepath.Join(program, updateDir)
	if e := os.MkdirAll(work, 0o700); e != nil {
		return "", fmt.Errorf("程序目录不可写（%w），请用完整包升级", e)
	}
	pkg := filepath.Join(work, "ImageWorkbench-"+m.Version+".zip")
	partial := pkg + ".partial"
	_ = os.Remove(partial)
	req, e := http.NewRequestWithContext(s.ctx, "GET", m.URL, nil)
	if e != nil {
		return "", e
	}
	resp, e := updateClient().Do(req)
	if e != nil {
		return "", fmt.Errorf("无法下载更新包：%w", e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("下载更新包失败：HTTP %d", resp.StatusCode)
	}
	f, e := os.Create(partial)
	if e != nil {
		return "", e
	}
	n, e := io.Copy(f, &progressReader{r: io.LimitReader(resp.Body, m.Size+1), s: s})
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		os.Remove(partial)
		return "", fmt.Errorf("下载更新包中断：%w", e)
	}
	if n != m.Size {
		os.Remove(partial)
		return "", errors.New("更新包大小不符，可能下载不完整")
	}
	sum, e := update.FileSHA256(partial)
	if e != nil || !strings.EqualFold(sum, m.SHA256) {
		os.Remove(partial)
		return "", errors.New("更新包校验失败（SHA-256 不符），已删除")
	}
	_ = os.Remove(pkg)
	if e = os.Rename(partial, pkg); e != nil {
		return "", e
	}
	stage := filepath.Join(work, m.Version)
	_ = os.RemoveAll(stage)
	if e = update.Extract(pkg, stage); e != nil {
		os.RemoveAll(stage)
		return "", e
	}
	info, e := update.ReadInfo(filepath.Join(stage, "version.json"))
	if e != nil || info.Version != m.Version {
		os.RemoveAll(stage)
		return "", errors.New("更新包的版本与清单不一致")
	}
	if e = update.CheckComponents(stage, program); e != nil {
		os.RemoveAll(stage)
		return "", e
	}
	return stage, nil
}

type progressReader struct {
	r    io.Reader
	s    *Service
	n    int64
	last time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, e := p.r.Read(b)
	p.n += int64(n)
	if time.Since(p.last) > 300*time.Millisecond || e != nil {
		p.last = time.Now()
		p.s.mu.Lock()
		p.s.upd.status.Downloaded = p.n
		p.s.mu.Unlock()
		p.s.signal()
	}
	return n, e
}

// StartUpdate hands over to the downloaded program: it waits for this process to
// exit, replaces the files and restarts. The caller quits the app right after.
// Refused while anything is generating or analysing.
func (s *Service) StartUpdate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upd.status.Stage != "ready" || s.upd.staged == "" {
		return errors.New("新版本还没有下载完成")
	}
	if e := s.agentBusy(); e != nil {
		return errors.New("创作正在进行，请等它完成后再更新")
	}
	counts, e := s.store.taskStatusCounts(false)
	if e != nil {
		return e
	}
	for status, n := range counts {
		if n > 0 && active(status) {
			return errors.New("还有图片在生成或下载，请等它们完成后再更新")
		}
	}
	exe := filepath.Join(s.upd.staged, "ImageWorkbench.exe")
	cmd := exec.Command(exe, updateFlag, "-pid", fmt.Sprint(os.Getpid()), "-program", s.programDir, "-stage", s.upd.staged)
	if e = cmd.Start(); e != nil {
		return fmt.Errorf("无法启动更新程序：%w", e)
	}
	_ = s.store.appendLog("info", "app", "", "开始更新到 "+s.upd.manifest.Version+"，软件将重新启动")
	s.paused = true
	return nil
}

// IsUpdateCommand reports whether the program was started to apply an update.
func IsUpdateCommand(args []string) bool { return len(args) > 1 && args[1] == updateFlag }

// RunUpdateCommand is the new program applying itself: wait for the old process,
// move the files in (with backup and rollback) and start the program again.
func RunUpdateCommand(args []string, waitExit func(pid int, timeout time.Duration)) error {
	var pid int
	var program, stage string
	for i := 2; i+1 < len(args); i += 2 {
		switch args[i] {
		case "-pid":
			fmt.Sscan(args[i+1], &pid)
		case "-program":
			program = args[i+1]
		case "-stage":
			stage = args[i+1]
		}
	}
	if program == "" || stage == "" {
		return errors.New("更新参数不完整")
	}
	if rel, e := filepath.Rel(filepath.Join(program, updateDir), stage); e != nil || strings.HasPrefix(rel, "..") {
		return errors.New("更新文件不在程序目录中")
	}
	if pid > 0 {
		waitExit(pid, time.Minute)
	}
	backup := filepath.Join(program, updateDir, "backup-"+time.Now().Format("20060102-150405"))
	applyErr := update.Apply(stage, program, backup)
	log := filepath.Join(program, updateDir, "update.log")
	msg := Now() + " 更新完成\r\n"
	if applyErr != nil {
		msg = Now() + " 更新失败，已恢复原文件：" + applyErr.Error() + "\r\n"
	}
	if f, e := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); e == nil {
		f.WriteString(msg)
		f.Close()
	}
	_ = exec.Command(filepath.Join(program, "ImageWorkbench.exe")).Start()
	return applyErr
}

// CleanupUpdates removes finished staging folders and keeps only the latest
// backup, at start; failures are ignored.
func CleanupUpdates(program string) {
	entries, _ := os.ReadDir(filepath.Join(program, updateDir))
	backups := []string{}
	for _, d := range entries {
		name := d.Name()
		path := filepath.Join(program, updateDir, name)
		switch {
		case strings.HasPrefix(name, "backup-"):
			backups = append(backups, path)
		case d.IsDir() || strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".partial"):
			_ = os.RemoveAll(path)
		}
	}
	for i := 0; i+1 < len(backups); i++ {
		_ = os.RemoveAll(backups[i])
	}
}

// updateClient follows redirects (GitHub Releases answers both update.json and
// the package with a redirect to its download host) but never from https down to
// http, and at most 10 times. Trust never comes from the host: update.json must
// carry the provider's signature and the package must match its SHA-256 and size.
func updateClient() *http.Client {
	return &http.Client{Transport: newNetworkTransport(), CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("更新地址跳转次数过多")
		}
		if via[0].URL.Scheme == "https" && r.URL.Scheme != "https" {
			return errors.New("更新地址从 https 跳转到了不安全的地址，已停止")
		}
		return nil
	}}
}
