package core

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestChangeDataDirectoryPreservesDataKeysAndOldLocation(t *testing.T) {
	s := testService(t)
	source := s.Root()
	c := saveConnection(t, s, "http://localhost:8090/v1")
	asset, e := s.ImportImage("商品.png", base64.StdEncoding.EncodeToString(tinyPNG()))
	if e != nil {
		t.Fatal(e)
	}
	s.SaveProject(Project{Name: "迁移项目"})
	target := filepath.Join(t.TempDir(), "自选 目录")
	saved := ""
	if e = s.ChangeDataDirectory(target, func(path string) error { saved = path; return nil }); e != nil {
		t.Fatal(e)
	}
	want, _ := filepath.EvalSymlinks(target)
	if s.Root() != want || saved != want {
		t.Fatal("location not switched")
	}
	if _, e = s.ReadAsset(asset.ID, false); e != nil {
		t.Fatal("image not migrated", e)
	}
	var full Connection
	s.store.Get("connection", c.ID, &full)
	key, e := s.key(full.Credential)
	if e != nil || key != "test-key" {
		t.Fatal("encrypted key not retained", e)
	}
	if _, e = os.Stat(filepath.Join(source, "app.db")); e != nil {
		t.Fatal("old data removed")
	}
	next, e := NewService(target)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Dispose()
	projects, _ := next.ListProjects()
	if len(projects) != 1 {
		t.Fatal("restart lost data")
	}
}
func TestChangeDataDirectoryRejectsBusyOccupiedNestedAndFailedPreference(t *testing.T) {
	s := testService(t)
	source := s.Root()
	persist := func(string) error { return nil }
	occupied := t.TempDir()
	sentinel := filepath.Join(occupied, "keep.txt")
	os.WriteFile(sentinel, []byte("keep"), 0600)
	for _, target := range []string{source, filepath.Dir(source), filepath.Join(source, "nested"), occupied} {
		if e := s.ChangeDataDirectory(target, persist); e == nil {
			t.Fatal("unsafe target accepted", target)
		}
	}
	data, _ := os.ReadFile(sentinel)
	if string(data) != "keep" {
		t.Fatal("existing file overwritten")
	}
	target := filepath.Join(t.TempDir(), "persist-failure")
	if e := s.ChangeDataDirectory(target, func(string) error { return errors.New("readonly config") }); e == nil {
		t.Fatal("persist error ignored")
	}
	if s.Root() != source {
		t.Fatal("changed after failure")
	}
	c := saveConnection(t, s, "http://localhost:8090/v1")
	s.Submit(request(c))
	if e := s.ChangeDataDirectory(filepath.Join(t.TempDir(), "busy"), persist); e == nil {
		t.Fatal("busy queue accepted")
	}
}
