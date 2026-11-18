package core

import (
	"path/filepath"
	"testing"
)

// Service tests run as on a Chinese Windows unless a test says otherwise, so
// they do not depend on the language of the machine running them.
func init() { systemUILanguages = func() []string { return []string{"zh-CN"} } }

func withSystemLanguage(t *testing.T, tags ...string) {
	t.Helper()
	old := systemUILanguages
	systemUILanguages = func() []string { return tags }
	t.Cleanup(func() { systemUILanguages = old })
}

func TestFirstStartFollowsWindowsLanguage(t *testing.T) {
	for _, c := range []struct {
		system []string
		want   string
	}{
		{[]string{"ja-JP"}, "ja"},
		{[]string{"ko-KR", "en-US"}, "ko"},
		{[]string{"vi-VN"}, "vi"},
		{[]string{"en-GB"}, "en"},
		{[]string{"zh-CN"}, "zh-CN"},
		// Not one of the five: Chinese, even when English is a fallback.
		{[]string{"de-DE", "en-US"}, "zh-CN"},
		{nil, "zh-CN"},
	} {
		withSystemLanguage(t, c.system...)
		s := testService(t)
		if got := s.Language(); got != c.want {
			t.Errorf("system %v: language %q, want %q", c.system, got, c.want)
		}
	}
}

func TestLanguageIsKeptAcrossRestarts(t *testing.T) {
	withSystemLanguage(t, "ja-JP")
	root := filepath.Join(t.TempDir(), "data")
	s, e := NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	if s.Language() != "ja" {
		t.Fatal("first start", s.Language())
	}
	if e = s.SetLanguage("ko"); e != nil || s.Language() != "ko" {
		t.Fatal(e, s.Language())
	}
	if s.SetLanguage("de") == nil || s.SetLanguage("zh") == nil || s.Language() != "ko" {
		t.Fatal("an unknown language was accepted", s.Language())
	}
	s.Dispose()
	// The saved choice wins over the system language on the next start.
	withSystemLanguage(t, "en-US")
	s, e = NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Dispose()
	if s.Language() != "ko" {
		t.Fatal("after restart", s.Language())
	}
}

// A data directory used before 0.8.0 has no language yet: it stays Chinese
// whatever Windows says, so an upgrade looks the same as before.
func TestUpgradeKeepsChinese(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	st, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = st.Put("preferences", "image-concurrency", 2); e != nil {
		t.Fatal(e)
	}
	st.Close()
	withSystemLanguage(t, "en-US")
	s, e := NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Dispose()
	if s.Language() != "zh-CN" {
		t.Fatal("upgrade switched language to", s.Language())
	}
}
