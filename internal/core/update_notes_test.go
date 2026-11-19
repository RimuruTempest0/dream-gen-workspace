package core

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"imageworkbench/internal/update"
)

// withNotesEn re-signs the served manifest with English notes added.
func withNotesEn(t *testing.T, srv *updateServer, en string) {
	t.Helper()
	var f struct {
		Payload string `json:"payload"`
	}
	json.Unmarshal(srv.manifest, &f)
	raw, _ := base64.StdEncoding.DecodeString(f.Payload)
	var m update.Manifest
	if e := json.Unmarshal(raw, &m); e != nil {
		t.Fatal(e)
	}
	m.NotesEn = en
	var e error
	if srv.manifest, e = update.Sign(m, srv.priv); e != nil {
		t.Fatal(e)
	}
}

// The one-click update shows the Chinese notes in a Chinese interface and the
// English ones in every other interface language; it follows a switch without
// checking again.
func TestUpdateNotesFollowTheInterfaceLanguage(t *testing.T) {
	s := testService(t)
	_, srv := prepareUpdate(t, s, "9.9.9", "", false)
	withNotesEn(t, srv, "Test update")
	if st, e := s.CheckForUpdate(); e != nil || st.Notes != "测试更新" || st.NotesLang != "zh-CN" {
		t.Fatalf("zh-CN %+v %v", st, e)
	}
	for _, lang := range []string{"en", "vi", "ja", "ko"} {
		if e := s.SetLanguage(lang); e != nil {
			t.Fatal(e)
		}
		if st := s.UpdateStatus(); st.Notes != "Test update" || st.NotesLang != "en" || st.Version != "9.9.9" {
			t.Fatalf("%s %+v", lang, st)
		}
	}
	s.SetLanguage("zh-CN")
	if st := s.UpdateStatus(); st.Notes != "测试更新" || st.NotesLang != "zh-CN" {
		t.Fatalf("back to zh-CN %+v", st)
	}
}

// A manifest without English notes (published before 0.8.0) shows the Chinese
// notes in every language.
func TestUpdateNotesFallBackToChinese(t *testing.T) {
	s := testService(t)
	prepareUpdate(t, s, "9.9.9", "", false)
	s.SetLanguage("ja")
	if st, e := s.CheckForUpdate(); e != nil || st.Notes != "测试更新" || st.NotesLang != "zh-CN" {
		t.Fatalf("%+v %v", st, e)
	}
}
