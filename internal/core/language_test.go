package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

// A 0.7.x data directory may hold no preference at all, only connections,
// tasks, assets or logs. Anything already saved counts as used before 0.8.0.
func TestUpgradeWithoutPreferencesKeepsChinese(t *testing.T) {
	for name, fill := range map[string]func(*Store) error{
		"connection": func(st *Store) error {
			return st.Put("connection", "c1", Connection{ID: "c1", Name: "旧连接", BaseURL: "https://example.com/v1"})
		},
		"task": func(st *Store) error {
			return st.Put("task", "t1", Task{ID: "t1", Status: "succeeded", Prompt: "一杯茶"})
		},
		"asset": func(st *Store) error { return st.Put("asset", "a1", Asset{ID: "a1"}) },
		"log":   func(st *Store) error { return st.appendLog("info", "app", "", "旧日志") },
	} {
		root := filepath.Join(t.TempDir(), "data")
		st, e := Open(root)
		if e != nil {
			t.Fatal(e)
		}
		if e = fill(st); e != nil {
			t.Fatal(name, e)
		}
		st.Close()
		withSystemLanguage(t, "en-US")
		s, e := NewService(root)
		if e != nil {
			t.Fatal(e)
		}
		got := s.Language()
		s.Dispose()
		if got != "zh-CN" {
			t.Errorf("old directory with only a %s: language %q, want zh-CN", name, got)
		}
	}
}

// A task started before a language switch finishes in its own language: the
// generation request and the task's error carry the task's captured language,
// not the language current when the queue gets to it.
func TestGenerationKeepsTheTaskLanguage(t *testing.T) {
	s := testService(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Write([]byte(`{"data":[{"id":"test-image"}]}`))
			return
		}
		// A refused key with no message of its own: the provider and the queue
		// both write the explanation.
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{}}`))
	}))
	defer server.Close()
	c := gatewayConnection(t, s, "relay", server.URL+"/v1")
	submitN(t, s, c, 1) // started in Chinese, still waiting in the queue
	if e := s.SetLanguage("en"); e != nil {
		t.Fatal(e)
	}
	s.Start(func() {})
	waitFor(t, "the zh task to fail", func() bool { return countStatus(s, "failed") == 1 })
	failed := tasksWhere(s, func(t Task) bool { return t.Status == "failed" })[0]
	if failed.Language != "zh-CN" || !strings.HasPrefix(failed.Error, "API Key 无效") || !strings.Contains(failed.Error, "服务拒绝请求") || strings.Contains(failed.Error, "refused") {
		t.Fatalf("task started in Chinese finished as %q: %s", failed.Language, failed.Error)
	}
	// The next task is started in English and fails in English.
	submitN(t, s, c, 1)
	waitFor(t, "the en task to fail", func() bool { return countStatus(s, "failed") == 2 })
	for _, task := range tasksWhere(s, func(t Task) bool { return t.Status == "failed" && t.ID != failed.ID }) {
		if task.Language != "en" || !strings.Contains(task.Error, "The service refused the request") || hasHan(task.Error) {
			t.Fatalf("task started in English finished as %q: %s", task.Language, task.Error)
		}
	}
}
