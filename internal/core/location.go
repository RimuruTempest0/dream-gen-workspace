package core

import (
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
			return s.fail("data.location.busy")
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
		return s.fail("data.location.choose")
	}
	if strings.HasPrefix(target, `\\`) {
		return s.fail("data.location.local")
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
		return s.fail("data.location.same")
	}
	if strings.HasPrefix(a, strings.TrimRight(b, sep)+sep) || strings.HasPrefix(b, strings.TrimRight(a, sep)+sep) {
		return s.fail("data.location.nested")
	}
	if entries, err := os.ReadDir(target); err == nil {
		if len(entries) > 0 {
			return s.fail("data.location.notEmpty")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if e = os.MkdirAll(target, 0700); e != nil {
		return s.fail("data.location.unwritable", "error", e)
	}
	if e = s.store.Snapshot(filepath.Join(target, "app.db")); e != nil {
		return s.fail("data.location.copyDatabase", "error", e)
	}
	for _, dir := range []string{"images", "references", "thumbnails", "secrets", "skills"} {
		if e = os.Mkdir(filepath.Join(target, dir), 0700); e != nil {
			return s.fail("data.location.incomplete", "error", e)
		}
		e = filepath.WalkDir(filepath.Join(source, dir), func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				return s.fail("data.location.links")
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
			return s.fail("data.location.copyFiles", "error", e)
		}
	}
	next, e := Open(target)
	if e != nil {
		return s.fail("data.location.openDatabase", "error", e)
	}
	var check string
	if e = next.db.QueryRow("PRAGMA integrity_check").Scan(&check); e != nil || check != "ok" {
		next.Close()
		return s.fail("data.location.check")
	}
	assets, e := List[Asset](next, "asset")
	if e != nil {
		next.Close()
		return e
	}
	for _, asset := range assets {
		if _, e = readOwned(target, asset.Path); e != nil {
			next.Close()
			return s.fail("data.location.imageCheck", "name", asset.Name)
		}
	}
	if e = persist(target); e != nil {
		next.Close()
		return s.fail("data.location.savePreference", "error", e)
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
