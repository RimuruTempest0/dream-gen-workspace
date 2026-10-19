package core

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Copy a consistent snapshot and encrypted keys; retain the original directory.
func (s *Service) ChangeDataDirectory(destination string, persist func(string) error) error {
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
			return errors.New("请先停止待执行任务，并等待正在生成或下载的任务结束，再更改保存位置")
		}
	}
	source, e := filepath.Abs(s.store.Root)
	if e != nil {
		return e
	}
	source, e = filepath.EvalSymlinks(source)
	if e != nil {
		return e
	}
	target, e := filepath.Abs(strings.TrimSpace(destination))
	if e != nil || strings.TrimSpace(destination) == "" {
		return errors.New("请选择本地保存目录")
	}
	if strings.HasPrefix(target, `\\`) {
		return errors.New("请选择本地磁盘目录，暂不支持网络共享")
	}
	if _, err := os.Stat(target); err == nil {
		target, e = filepath.EvalSymlinks(target)
	} else if os.IsNotExist(err) {
		parent, err := filepath.EvalSymlinks(filepath.Dir(target))
		if err != nil {
			return err
		}
		target = filepath.Join(parent, filepath.Base(target))
	} else {
		return err
	}
	if e != nil {
		return e
	}
	a, b := strings.ToLower(filepath.Clean(source)), strings.ToLower(filepath.Clean(target))
	sep := string(filepath.Separator)
	if a == b {
		return errors.New("这个目录已经是当前数据位置")
	}
	if strings.HasPrefix(a, strings.TrimRight(b, sep)+sep) || strings.HasPrefix(b, strings.TrimRight(a, sep)+sep) {
		return errors.New("新目录不能是当前数据目录的上级或子目录，请选择独立的空文件夹")
	}
	if entries, err := os.ReadDir(target); err == nil {
		if len(entries) > 0 {
			return errors.New("新保存目录必须为空，以免覆盖已有文件。请创建或选择一个空文件夹")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if e = os.MkdirAll(target, 0700); e != nil {
		return fmt.Errorf("新目录无法写入：%w", e)
	}
	if e = s.store.Snapshot(filepath.Join(target, "app.db")); e != nil {
		return fmt.Errorf("复制数据库失败，仍使用原目录：%w", e)
	}
	for _, dir := range []string{"images", "references", "thumbnails", "secrets", "skills"} {
		if e = os.Mkdir(filepath.Join(target, dir), 0700); e != nil {
			return fmt.Errorf("迁移未完成，仍使用原目录：%w", e)
		}
		e = filepath.WalkDir(filepath.Join(source, dir), func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				return errors.New("数据目录包含链接，无法安全迁移")
			}
			if path == filepath.Join(source, dir) {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".partial-") {
				return nil
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			dest, err := owned(target, rel)
			if err != nil {
				return err
			}
			if d.IsDir() {
				return os.Mkdir(dest, 0700)
			}
			return copyExclusive(path, dest)
		})
		if e != nil {
			return fmt.Errorf("复制文件失败，仍使用原目录。目标可能留有不完整副本：%w", e)
		}
	}
	next, e := Open(target)
	if e != nil {
		return fmt.Errorf("新目录数据库无法打开，仍使用原目录：%w", e)
	}
	var check string
	if e = next.db.QueryRow("PRAGMA integrity_check").Scan(&check); e != nil || check != "ok" {
		next.Close()
		return errors.New("新目录的数据校验失败，仍使用原目录")
	}
	assets, e := List[Asset](next, "asset")
	if e != nil {
		next.Close()
		return e
	}
	for _, asset := range assets {
		if _, e = readOwned(target, asset.Path); e != nil {
			next.Close()
			return fmt.Errorf("图片迁移校验失败，仍使用原目录：%s", asset.Name)
		}
	}
	if e = persist(target); e != nil {
		next.Close()
		return fmt.Errorf("保存目录偏好失败，仍使用原目录：%w", e)
	}
	old := s.store
	s.store = next
	old.Close()
	s.signal()
	return nil
}
func copyExclusive(source, target string) error {
	input, e := os.Open(source)
	if e != nil {
		return e
	}
	defer input.Close()
	output, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(output, input)
	if e == nil {
		e = output.Sync()
	}
	closeErr := output.Close()
	if e != nil {
		return e
	}
	return closeErr
}
