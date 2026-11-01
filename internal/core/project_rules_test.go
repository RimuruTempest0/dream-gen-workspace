package core

import (
	"context"
	"encoding/json"
	"fmt"
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
	for _, b := range builtinSkills[:3] {
		var rec SkillRecord
		s.store.Get("skill", b.id, &rec)
		rec.Content = shippedContent(b.id, embeddedSkillText("builtin_skills/history/"+b.id+"-0.3.5.md"))
		s.store.Put("skill", rec.ID, rec)
	}
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	for _, b := range builtinSkills[:3] {
		var rec SkillRecord
		s.store.Get("skill", b.id, &rec)
		if !strings.HasPrefix(b.body, "如果本次还启用了项目规则技能") || rec.Content != b.content() {
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
	if projectRulesSuffix(AgentRun{Skills: []SkillRecord{{ID: p004SkillID, Content: old}}}, SuitePlan{}) == "" {
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

func TestImagePromptingSkillAddedAndSuiteUpgradedFrom050(t *testing.T) {
	s := testService(t)
	b := builtinSkills[4]
	if _, _, e := validateSkill(b.content()); e != nil {
		t.Fatal("image-prompting fails validation", e)
	}
	// Simulate a 0.5.0 data folder: offered the first four skills, etsy-suite unedited.
	s.store.Delete("skill", b.id)
	s.store.Put("preferences", "builtin-skills-offered", builtinSkillIDs()[:4])
	var suite SkillRecord
	s.store.Get("skill", "etsy-suite", &suite)
	suite.Content = shippedContent("etsy-suite", embeddedSkillText("builtin_skills/history/etsy-suite-0.5.0.md"))
	suite.Version = 6
	s.store.Put("skill", suite.ID, suite)
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	var rec SkillRecord
	if s.store.Get("skill", b.id, &rec) != nil || !rec.Enabled || rec.Source != "内置" {
		t.Fatal("image-prompting not added to 0.5.0 data")
	}
	s.store.Get("skill", "etsy-suite", &suite)
	if suite.Version != 7 || suite.Content != builtinSkills[1].content() {
		t.Fatal("0.5.0 etsy-suite copy not upgraded", suite.Version)
	}
	for _, want := range []string{"image-prompting", "画面文字（逐字）", "参考图"} {
		if !strings.Contains(builtinSkills[1].body, want) {
			t.Fatal("etsy-suite does not use the prompt format", want)
		}
	}
	// The shared format must not reintroduce wording that P004 bans in prompts.
	for _, bad := range []string{"虚化", "景深", "柔焦", "散景"} {
		if strings.Contains(b.body, bad) {
			t.Fatal("image-prompting contains banned word", bad)
		}
	}
}

func TestP004CuffRulesFollowPlanCuffEmbroidery(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	run := func(cuff *bool) []Task {
		s.agentRunner = func(ctx context.Context, in agentInvocation) error {
			r := invocationRun(t, in)
			b := &agentBridge{service: s, runID: r.ID, ctx: ctx}
			p := simplePlan(12, []string{})
			args := map[string]any{"locked": p.Locked, "editable": p.Editable, "items": p.Items}
			// The final save of a P004 run must say whether the cuffs have embroidery.
			raw, _ := json.Marshal(args)
			if _, e := b.callTool("save_suite_plan", raw); e == nil || !strings.Contains(e.Error(), "cuffEmbroidery") {
				t.Error("plan without cuffEmbroidery accepted for a P004 run", e)
			}
			args["cuffEmbroidery"] = cuff
			raw, _ = json.Marshal(args)
			if _, e := b.callTool("save_suite_plan", raw); e != nil {
				return e
			}
			emitAgentSuccess(in)
			return nil
		}
		id, e := s.StartSmartGeneration(agentInput(llm, images), fmt.Sprint("cuff-", *cuff))
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
	no, yes := false, true
	for _, task := range run(&no) {
		if !strings.HasSuffix(task.Prompt, p004FixedRulesNoCuff) || strings.Contains(task.Prompt, "袖口刺绣只在") || strings.Contains(task.Prompt, "小图标") {
			t.Fatal("cuff clauses appended for a product without cuff embroidery", task.Prompt)
		}
	}
	for _, task := range run(&yes) {
		if !strings.HasSuffix(task.Prompt, p004FixedRules) {
			t.Fatal("cuff clauses missing for a product with cuff embroidery", task.Prompt)
		}
	}
	// Plans saved before 0.5.2 have no answer and keep the full rules.
	shipped := AgentRun{Skills: []SkillRecord{{ID: p004SkillID, Content: builtinSkillContent(p004SkillID, builtinSkills[3].description, builtinSkills[3].body)}}}
	if projectRulesSuffix(shipped, SuitePlan{}) != "\n"+p004FixedRules {
		t.Fatal("old plan lost the full P004 rules")
	}
	if !strings.Contains(builtinSkills[3].body, "cuffEmbroidery") {
		t.Fatal("p004-rules does not explain cuffEmbroidery")
	}
	// An unedited 0.5.1 copy still counts as shipped and is upgraded.
	old := builtinSkillContent(p004SkillID, builtinSkills[3].description, embeddedSkillText("builtin_skills/history/p004-rules-0.5.1.md"))
	if projectRulesSuffix(AgentRun{Skills: []SkillRecord{{ID: p004SkillID, Content: old}}}, SuitePlan{CuffEmbroidery: &no}) != "\n"+p004FixedRulesNoCuff {
		t.Fatal("0.5.1 p004 copy lost the fixed rules")
	}
	var rec SkillRecord
	s.store.Get("skill", p004SkillID, &rec)
	rec.Content = old
	s.store.Put("skill", rec.ID, rec)
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	s.store.Get("skill", p004SkillID, &rec)
	if rec.Content != shipped.Skills[0].Content {
		t.Fatal("0.5.1 p004 copy not upgraded")
	}
}

func TestSkillsFrom054AreUpgradedWithDesignConsistencyRules(t *testing.T) {
	s := testService(t)
	ids := map[string]int{"product-analysis": 0, "etsy-suite": 1, "image-prompting": 4}
	for id := range ids {
		var rec SkillRecord
		s.store.Get("skill", id, &rec)
		rec.Content = shippedContent(id, embeddedSkillText("builtin_skills/history/"+id+"-0.5.4.md"))
		rec.Version = 3
		s.store.Put("skill", rec.ID, rec)
	}
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	for id, i := range ids {
		var rec SkillRecord
		s.store.Get("skill", id, &rec)
		if rec.Version != 4 || rec.Content != builtinSkills[i].content() {
			t.Fatal("0.5.4 copy not upgraded", id, rec.Version)
		}
	}
	// A changed design must name what the old references still show, in one closed list.
	for id, want := range map[int][]string{0: {"改款对照表", "去掉"}, 1: {"图案只包含", "图案里没有", "同一组参考图", "固定短名"}, 4: {"图案只包含", "图案里没有", "same short name", "same order"}} {
		for _, w := range want {
			if !strings.Contains(builtinSkills[id].body, w) {
				t.Fatal("design consistency rule missing", builtinSkills[id].id, w)
			}
		}
	}
	for _, b := range builtinSkills {
		for _, bad := range []string{"虚化", "景深", "柔焦", "散景"} {
			if b.id != p004SkillID && strings.Contains(b.body, bad) {
				t.Fatal("banned blur word in", b.id, bad)
			}
		}
	}
}

// shippedContent rebuilds an earlier shipped copy: etsy-suite then had its Etsy-only
// description and slug.
func shippedContent(id, body string) string {
	for _, b := range builtinSkills {
		if b.id == id && id == "etsy-suite" {
			return builtinSkillContent(id, etsySuiteDescriptionV2, body)
		} else if b.id == id {
			return builtinSkillContent(id, b.description, body)
		}
	}
	return ""
}

func TestSuiteSkillBecomesPlatformNeutralFrom067(t *testing.T) {
	s := testService(t)
	ids := map[string]int{"product-analysis": 0, "etsy-suite": 1, "image-prompting": 4}
	for id := range ids {
		var rec SkillRecord
		s.store.Get("skill", id, &rec)
		rec.Content = shippedContent(id, embeddedSkillText("builtin_skills/history/"+id+"-0.6.7.md"))
		if id == "etsy-suite" {
			rec.Name = etsySuiteNameV1
		}
		rec.Version = 9
		s.store.Put("skill", rec.ID, rec)
	}
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	for id, i := range ids {
		var rec SkillRecord
		s.store.Get("skill", id, &rec)
		if rec.Version != 10 || rec.Content != builtinSkills[i].content() || rec.Name != builtinSkills[i].name {
			t.Fatal("0.6.7 copy not upgraded", id, rec.Version, rec.Name)
		}
	}
	suite := builtinSkills[1]
	if slug, _, e := validateSkill(suite.content()); e != nil || slug != "listing-suite" || suite.name != "电商套图规划" {
		t.Fatal("suite skill not renamed", slug, e)
	}
	for _, want := range []string{"Amazon", "Walmart", "主题元素", "微距特写", "只用有依据的选项", "内部代号", "从左到右依次是"} {
		if !strings.Contains(suite.body, want) {
			t.Fatal("listing-suite rule missing", want)
		}
	}
	for _, want := range []string{"Scenes and themes", "internal codes", "at least 3 people"} {
		if !strings.Contains(builtinSkills[4].body, want) {
			t.Fatal("image-prompting rule missing", want)
		}
	}
	if !strings.Contains(builtinSkills[0].body, "可选项清单") {
		t.Fatal("product-analysis option sources missing")
	}
	// The skills that point at the suite skill use its new name.
	for _, b := range builtinSkills {
		if strings.Contains(b.body, "etsy-suite") {
			t.Fatal("stale etsy-suite reference in", b.id)
		}
	}
	// A user-edited copy keeps its name and text.
	var rec SkillRecord
	s.store.Get("skill", "etsy-suite", &rec)
	rec.Name, rec.Content = "我的套图", rec.Content+"\n自定义"
	s.store.Put("skill", rec.ID, rec)
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	s.store.Get("skill", "etsy-suite", &rec)
	if rec.Name != "我的套图" || !strings.HasSuffix(rec.Content, "自定义") {
		t.Fatal("edited skill changed")
	}
}

func TestSkillsFrom0610AreUpgradedWithCuffSentence(t *testing.T) {
	s := testService(t)
	old := map[string]string{
		"etsy-suite":      builtinSkillContent(builtinSkills[1].slug, builtinSkills[1].description, embeddedSkillText("builtin_skills/history/listing-suite-0.6.10.md")),
		p004SkillID:       builtinSkillContent(p004SkillID, builtinSkills[3].description, embeddedSkillText("builtin_skills/history/p004-rules-0.6.10.md")),
		"image-prompting": builtinSkillContent("image-prompting", builtinSkills[4].description, embeddedSkillText("builtin_skills/history/image-prompting-0.6.10.md")),
	}
	for id, content := range old {
		var rec SkillRecord
		s.store.Get("skill", id, &rec)
		rec.Content, rec.Version = content, 2
		s.store.Put("skill", rec.ID, rec)
	}
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	for _, b := range builtinSkills {
		if _, ok := old[b.id]; !ok {
			continue
		}
		var rec SkillRecord
		s.store.Get("skill", b.id, &rec)
		if rec.Version != 3 || rec.Content != b.content() {
			t.Fatal("0.6.10 copy not upgraded", b.id, rec.Version)
		}
	}
	// A run frozen with the unedited 0.6.10 P004 copy still gets the fixed rules.
	if projectRulesSuffix(AgentRun{Skills: []SkillRecord{{ID: p004SkillID, Content: old[p004SkillID]}}}, SuitePlan{}) == "" {
		t.Fatal("0.6.10 p004 copy lost the fixed rules")
	}
	for i, want := range map[int][]string{1: {"袖口句", "前臂在画面里竖直", "不能写“袖口不显示名字”"}, 3: {"袖口句"}, 4: {"One fixed sentence for small grouped details", "forearm", "Do not hide what the frame shows"}} {
		for _, w := range want {
			if !strings.Contains(builtinSkills[i].body, w) {
				t.Fatal("cuff consistency rule missing", builtinSkills[i].id, w)
			}
		}
	}
	if strings.Contains(builtinSkills[1].body, "袖口细节不显示") {
		t.Fatal("color chart still asks to hide a visible cuff")
	}
}
