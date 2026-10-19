package core

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Service) CreateBackup(destination string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.agentBusy(); e != nil {
		return e
	}
	s.paused = true
	defer func() { s.paused = false; s.signal() }()
	temp, e := os.MkdirTemp(filepath.Dir(s.store.Root), ".backup-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(temp)
	if e = s.store.Snapshot(filepath.Join(temp, "app.db")); e != nil {
		return e
	}
	snapshot, e := Open(temp)
	if e != nil {
		return e
	}
	if e = sanitizeBackup(snapshot); e != nil {
		snapshot.Close()
		return e
	}
	if e = snapshot.Close(); e != nil {
		return e
	}
	file, e := os.CreateTemp(filepath.Dir(destination), ".backup-*.zip")
	if e != nil {
		return e
	}
	name := file.Name()
	defer os.Remove(name)
	writer := zip.NewWriter(file)
	add := func(source, entry string) error {
		f, e := os.Open(source)
		if e != nil {
			return e
		}
		defer f.Close()
		w, e := writer.Create(entry)
		if e != nil {
			return e
		}
		_, e = io.Copy(w, f)
		return e
	}
	e = add(filepath.Join(temp, "app.db"), "app.db")
	if e == nil {
		for _, dir := range []string{"images", "references", "thumbnails", "skills"} {
			e = filepath.WalkDir(filepath.Join(s.store.Root, dir), func(path string, d os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.Type()&os.ModeSymlink != 0 {
					return errors.New("备份目录中存在符号链接")
				}
				if d.IsDir() || strings.HasPrefix(d.Name(), ".partial-") {
					return nil
				}
				rel, e := filepath.Rel(s.store.Root, path)
				if e != nil {
					return e
				}
				return add(path, filepath.ToSlash(rel))
			})
			if e != nil {
				break
			}
		}
	}
	if closeErr := writer.Close(); e == nil {
		e = closeErr
	}
	if syncErr := file.Sync(); e == nil {
		e = syncErr
	}
	if closeErr := file.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(name, destination)
}
func sanitizeBackup(st *Store) error {
	runs, e := List[AgentRun](st, "agent_run")
	if e != nil {
		return e
	}
	for _, r := range runs {
		r.LLMConnection.Credential = ""
		r.LLMConnection.HasKey = false
		r.LLMConnection.Capabilities = LLMCapabilities{}
		r.ImageConnection.Credential = ""
		r.ImageConnection.HasKey = false
		r.AnalysisSucceeded = false
		r.CancelRequested = false
		if r.Status == "analyzing" {
			r.Status = "interrupted"
			r.Error = "备份恢复后请配置密钥并明确继续"
		}
		if e = st.Put("agent_run", r.ID, r); e != nil {
			return e
		}
	}
	cs, e := List[Connection](st, "connection")
	if e != nil {
		return e
	}
	for _, c := range cs {
		c.Credential = ""
		c.HasKey = false
		c.Capabilities = LLMCapabilities{}
		if e = st.Put("connection", c.ID, c); e != nil {
			return e
		}
	}
	ts, e := List[Task](st, "task")
	if e != nil {
		return e
	}
	for _, t := range ts {
		t.Connection.Credential = ""
		t.Connection.HasKey = false
		t.Results = nil
		switch t.Status {
		case "queued", "blocked":
			t.Status = "blocked"
			t.Error = "恢复后请重新保存连接密钥，并显式恢复未提交的任务"
		case "submitting", "running":
			t.Status = "unknown"
			t.Error = "备份时正在提交，结果未知，请核对平台记录"
		case "downloading", "download_failed":
			t.Status = "download_failed"
			t.Error = "备份不保留临时图片链接，已归档图片仍在素材库中"
		}
		if e = st.Put("task", t.ID, t); e != nil {
			return e
		}
	}
	_, e = st.db.Exec("VACUUM")
	return e
}
func (s *Service) RestoreBackup(source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.agentBusy(); e != nil {
		return e
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return e
	}
	for _, t := range tasks {
		if active(t.Status) {
			return errors.New("请先停止待执行任务，并等待活动任务结束后恢复")
		}
	}
	s.paused = true
	defer func() { s.paused = false; s.signal() }()
	zipFile, e := zip.OpenReader(source)
	if e != nil {
		return errors.New("备份不是有效 ZIP 文件")
	}
	defer zipFile.Close()
	temp, e := os.MkdirTemp(filepath.Dir(s.store.Root), ".restore-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(temp)
	var total uint64
	hasDB := false
	seen := map[string]bool{}
	for _, f := range zipFile.File {
		if f.FileInfo().IsDir() {
			continue
		}
		n := f.Name
		valid := n == "app.db" || strings.HasPrefix(n, "images/") || strings.HasPrefix(n, "references/") || strings.HasPrefix(n, "thumbnails/") || strings.HasPrefix(n, "skills/")
		if !valid || strings.Contains(n, "\\") || f.Mode()&os.ModeSymlink != 0 {
			return errors.New("备份包含非法文件")
		}
		if seen[n] {
			return errors.New("备份包含重复文件")
		}
		seen[n] = true
		total += f.UncompressedSize64
		if total > 4<<30 || f.UncompressedSize64 > 256<<20 {
			return errors.New("备份文件过大")
		}
		path, e := owned(temp, n)
		if e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		out, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			r.Close()
			return e
		}
		_, copyErr := io.Copy(out, io.LimitReader(r, 256<<20+1))
		closeErr := out.Close()
		r.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n == "app.db" {
			hasDB = true
		}
	}
	if !hasDB {
		return errors.New("备份缺少数据库")
	}
	check, e := Open(temp)
	if e != nil {
		return errors.New("备份数据库无法打开")
	}
	var integrity string
	e = check.db.QueryRow("PRAGMA integrity_check").Scan(&integrity)
	if e != nil || integrity != "ok" {
		check.Close()
		return errors.New("备份数据库校验失败")
	}
	if e = sanitizeBackup(check); e != nil {
		check.Close()
		return e
	}
	assets, e := List[Asset](check, "asset")
	if e != nil {
		check.Close()
		return e
	}
	for _, a := range assets {
		if _, e = readOwned(temp, a.Path); e != nil {
			check.Close()
			return fmt.Errorf("备份缺少图片：%s", a.Name)
		}
	}
	check.Close()
	root := s.store.Root
	old := root + ".before-restore-" + time.Now().Format("20060102-150405") + "-" + NewID()[:6]
	if e = s.store.Close(); e != nil {
		return e
	}
	if e = os.Rename(root, old); e != nil {
		s.store, _ = Open(root)
		return e
	}
	if e = os.Rename(temp, root); e != nil {
		os.Rename(old, root)
		s.store, _ = Open(root)
		return e
	}
	s.store, e = Open(root)
	if e != nil {
		return errors.New("恢复后的数据库启动失败，原数据保留于 " + old)
	}
	return s.initializeAgents()
}
