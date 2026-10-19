package core

import (
	"context"
	"strings"
	"testing"
)

func runSmartSet(t *testing.T, s *Service, llm, images Connection, request string) []Task {
	t.Helper()
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		r := invocationRun(t, in)
		if e := saveRunnerPlan(s, r, simplePlan(12, []string{})); e != nil {
			return e
		}
		emitAgentSuccess(in)
		return nil
	}
	id, e := s.StartSmartGeneration(agentInput(llm, images), request)
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	tasks, _ := s.GetAgentTasks(id)
	if len(tasks) != 12 {
		r, _ := s.GetAgentRun(id)
		t.Fatal("set not submitted", r.Status, r.Error)
	}
	return tasks
}

func TestProjectRulesAppendedOnlyWhileShippedSkillIsUsed(t *testing.T) {
	s := testService(t)
	var p004 SkillRecord
	if s.store.Get("skill", p004SkillID, &p004) != nil || !p004.Enabled {
		t.Fatal("p004-rules not installed and enabled on a new data folder")
	}
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	for _, task := range runSmartSet(t, s, llm, images, "with-rules") {
		if !strings.HasSuffix(task.Prompt, p004FixedRules) || strings.Count(task.Prompt, "项目固定规则") != 1 {
			t.Fatal("fixed project rules missing from image prompt", task.Prompt)
		}
	}
	// An edited copy speaks for itself: the backend does not append rules the user changed.
	p004.Content = strings.Replace(p004.Content, "25 cm", "20 cm", -1)
	if _, e := s.SaveSkill(p004); e != nil {
		t.Fatal(e)
	}
	for _, task := range runSmartSet(t, s, llm, images, "edited-rules") {
		if strings.Contains(task.Prompt, "项目固定规则") {
			t.Fatal("fixed rules appended for an edited skill")
		}
	}
	if e := s.SetSkillEnabled(p004SkillID, false); e != nil {
		t.Fatal(e)
	}
	for _, task := range runSmartSet(t, s, llm, images, "without-rules") {
		if strings.Contains(task.Prompt, "项目固定规则") {
			t.Fatal("fixed rules appended while the skill is disabled")
		}
	}
}

func TestNewBuiltinSkillIsAddedOnceToExistingData(t *testing.T) {
	s := testService(t)
	// Simulate a data folder set up before 0.3.6: initialized, no offered list, no p004-rules.
	s.store.Delete("skill", p004SkillID)
	s.store.Delete("preferences", "builtin-skills-offered")
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	var rec SkillRecord
	if s.store.Get("skill", p004SkillID, &rec) != nil || !rec.Enabled || rec.Source != "内置" {
		t.Fatal("p004-rules not added to existing data")
	}
	if e := s.DeleteSkill(p004SkillID); e != nil {
		t.Fatal(e)
	}
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	if s.store.Get("skill", p004SkillID, &rec) == nil {
		t.Fatal("a deleted built-in skill came back")
	}
	if _, _, e := validateSkill(builtinSkillContent(p004SkillID, builtinSkills[3].description, builtinSkills[3].body)); e != nil {
		t.Fatal("p004-rules fails validation", e)
	}
}

func TestSkillsFrom035AreUpgradedWithPriorityNote(t *testing.T) {
	s := testService(t)
	for i, b := range builtinSkills[:3] {
		var rec SkillRecord
		s.store.Get("skill", b.id, &rec)
		rec.Content = builtinSkillContent(b.id, builtinSkills[i].description, embeddedSkillText("builtin_skills/history/"+b.id+"-0.3.5.md"))
		s.store.Put("skill", rec.ID, rec)
	}
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	for _, b := range builtinSkills[:3] {
		var rec SkillRecord
		s.store.Get("skill", b.id, &rec)
		if !strings.HasPrefix(b.body, "如果本次还启用了项目规则技能") || rec.Content != builtinSkillContent(b.id, b.description, b.body) {
			t.Fatal("0.3.5 copy not upgraded with the priority note", b.id)
		}
	}
}

func TestIconOrderUsesReadingOrderOnly(t *testing.T) {
	for _, b := range builtinSkills {
		for _, bad := range []string{"靠肘部的一端", "靠肩膀的一端", "靠肘部一侧", "靠肩膀一侧"} {
			if strings.Contains(b.body, bad) {
				t.Fatal("ambiguous icon wording left in", b.id, bad)
			}
		}
	}
	if !strings.Contains(builtinSkills[3].body, "紧挨在名字首字母之前") || !strings.Contains(p004FixedRules, "左右顺序在每张图中完全相同") {
		t.Fatal("reading-order icon rule missing")
	}
	// A run frozen with the unedited 0.3.6 copy still gets the fixed rules after upgrading.
	old := builtinSkillContent(p004SkillID, builtinSkills[3].description, embeddedSkillText("builtin_skills/history/p004-rules-0.3.6.md"))
	if projectRulesSuffix(AgentRun{Skills: []SkillRecord{{ID: p004SkillID, Content: old}}}) == "" {
		t.Fatal("0.3.6 p004 copy lost the fixed rules")
	}
	s := testService(t)
	var rec SkillRecord
	s.store.Get("skill", p004SkillID, &rec)
	rec.Content = old
	s.store.Put("skill", rec.ID, rec)
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	s.store.Get("skill", p004SkillID, &rec)
	if rec.Content != builtinSkillContent(p004SkillID, builtinSkills[3].description, builtinSkills[3].body) {
		t.Fatal("0.3.6 p004 copy not upgraded")
	}
}
