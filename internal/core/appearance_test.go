package core

import (
	"path/filepath"
	"testing"
)

// A new data directory follows Windows; the choice is kept across restarts.
func TestAppearanceStartsOnSystemAndIsKept(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	s, e := NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	if s.Appearance() != AppearanceSystem {
		t.Fatal("first start", s.Appearance())
	}
	if e = s.SetAppearance(AppearanceLight); e != nil || s.Appearance() != AppearanceLight {
		t.Fatal(e, s.Appearance())
	}
	if s.SetAppearance("auto") == nil || s.SetAppearance("") == nil || s.Appearance() != AppearanceLight {
		t.Fatal("an unknown appearance was accepted", s.Appearance())
	}
	s.Dispose()
	s, e = NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Dispose()
	if s.Appearance() != AppearanceLight {
		t.Fatal("after restart", s.Appearance())
	}
}

// A data directory used before 0.8.3 stays dark, so an upgrade looks the same.
func TestUpgradeKeepsDarkAppearance(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	st, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = st.Put("preferences", interfaceLanguagePrefKey, "zh-CN"); e != nil {
		t.Fatal(e)
	}
	st.Close()
	s, e := NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Dispose()
	if s.Appearance() != AppearanceDark {
		t.Fatal("upgrade switched appearance to", s.Appearance())
	}
}
