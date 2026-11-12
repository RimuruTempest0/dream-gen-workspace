package core

import (
	"path/filepath"
	"strings"
	"testing"
)

// builtinMarker is a line of a built-in skill's text.
func builtinMarker(t *testing.T) string {
	t.Helper()
	best := ""
	for _, line := range strings.Split(builtinSkillBody("image-prompting"), "\n") {
		line = strings.TrimSpace(line)
		if len(line) > len(best) && !strings.ContainsAny(line, "\"<>&\\") {
			best = line
		}
	}
	if len(best) < 20 {
		t.Fatal("no marker line")
	}
	return best
}

// A data folder written by 0.7.10–0.7.16 in a customer package holds sealed stubs;
// opening it restores the full text in the skill list and in old runs.
func TestSealedBuiltinSkillsAreRestored(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	s, e := NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := builtinByID("image-prompting")
	stub := builtinSkillContent(b.slug, b.description, "（内置技能：正文随软件提供，不保存在数据目录。）")
	var rec SkillRecord
	s.store.Get("skill", b.id, &rec)
	rec.Content, rec.Sealed = stub, true
	s.store.Put("skill", rec.ID, rec)
	run := AgentRun{ID: NewID(), Status: "completed", Skills: []SkillRecord{rec}}
	s.store.Put("agent_run", run.ID, run)
	s.Dispose()

	s, e = NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Dispose)
	marker := builtinMarker(t)
	rec = SkillRecord{}
	s.store.Get("skill", b.id, &rec)
	if rec.Sealed || !strings.Contains(rec.Content, marker) {
		t.Fatalf("skill not restored: sealed=%v content=%.80q", rec.Sealed, rec.Content)
	}
	var got AgentRun
	s.store.Get("agent_run", run.ID, &got)
	if got.Skills[0].Sealed || !strings.Contains(got.Skills[0].Content, marker) {
		t.Fatal("run's frozen skill not restored")
	}
}

// Customer packages show, edit and delete built-in skills like the developer build.
func TestCustomerPackageShowsBuiltinSkills(t *testing.T) {
	isolateLicenseState(t)
	_, server := newKeyRelay(t)
	s := testService(t)
	c := writeCustomer(t, server.URL+"/v1", 3, 1, "line-image-1", nil)
	if e := s.LoadLicense(c.dir); e != nil {
		t.Fatal(e)
	}
	marker := builtinMarker(t)
	skills, _ := s.ListSkills()
	shown := false
	for _, v := range skills {
		if v.ID == "image-prompting" {
			shown = strings.Contains(v.Content, marker)
		}
	}
	if !shown {
		t.Fatal("customer package hides built-in text")
	}
	if e := s.SetSkillEnabled(p004SkillID, false); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SaveSkill(SkillRecord{ID: "image-prompting", Name: "x", Content: "---\nname: image-prompting\ndescription: x\n---\n\nx", Enabled: true}); e != nil {
		t.Fatal(e)
	}
}
