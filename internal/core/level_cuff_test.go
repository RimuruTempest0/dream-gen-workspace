package core

import (
	"strings"
	"testing"
)

func TestCuffTextIsLevelNotTilted(t *testing.T) {
	if strings.Contains(p004FixedRules, "倾斜") && !strings.Contains(p004FixedRules, "不倾斜") || !strings.Contains(p004FixedRules, "水平摆正") {
		t.Fatal("P004 fixed rules must keep cuff text level", p004FixedRules)
	}
	for _, b := range builtinSkills {
		for _, bad := range []string{p004TiltWording, "一起斜", "不单独摆正"} {
			if strings.Contains(b.body, bad) {
				t.Fatal("tilted cuff wording left in", b.id, bad)
			}
		}
	}
	if !strings.Contains(builtinSkills[3].body, "水平摆正、不倾斜") {
		t.Fatal("P004 locked sentence does not keep the text level")
	}
	// Refining an image planned before 0.6.13 drops the tilt from its old prompt.
	old := "统一锁定项：袖口文字基线与该袖口接缝线平行并随袖口角度倾斜，整组居中\n项目固定规则（P004）：袖口刺绣只在最终画面右侧那只袖子的袖口，文字基线与该袖口接缝线平行并随袖口角度倾斜，整组位于袖筒宽度中心"
	p := refinePrompt(old, "把袖口文字摆正", false)
	if strings.Contains(p, p004TiltWording) || strings.Count(p, p004LevelWording) != 2 {
		t.Fatal("refine kept the tilt wording", p)
	}
}

func TestItemPromptScopesTheProductSpec(t *testing.T) {
	shipped := AgentRun{Input: AgentInput{Ratio: "1:1"}, Skills: []SkillRecord{{ID: p004SkillID, Content: builtinSkills[3].content()}}}
	cuff := true
	p := SuitePlan{Locked: []string{"卫衣整件规格", "袖口狗掌和名字"}, Editable: []string{"背景"}, CuffEmbroidery: &cuff}
	chart := itemPrompt(shipped, p, SuiteItem{Prompt: "线色卡", NoProduct: true})
	if strings.Contains(chart, "统一锁定项") || strings.Contains(chart, "袖口") || !strings.Contains(chart, p004ClarityRules) || !strings.HasPrefix(chart, "线色卡") {
		t.Fatal("a no-product image must not get the garment spec", chart)
	}
	sample := itemPrompt(shipped, p, SuiteItem{Prompt: "袖口特写", Sample: true})
	if !strings.HasPrefix(sample, sampleSelfLine) || !strings.Contains(sample, "统一锁定项：卫衣整件规格") || !strings.HasSuffix(sample, p004FixedRules) {
		t.Fatal("the sample needs its close-up line and the full spec", sample)
	}
	normal := itemPrompt(shipped, p, SuiteItem{Prompt: "客厅"})
	if normal != "客厅\n统一锁定项：卫衣整件规格；袖口狗掌和名字\n允许修改：背景\n画面比例：1:1\n"+p004FixedRules {
		t.Fatal("ordinary prompt changed", normal)
	}
	s := testService(t)
	r := AgentRun{Input: AgentInput{References: []string{}}}
	for _, bad := range []SuitePlan{
		{Items: []SuiteItem{{Prompt: "p", NoProduct: true, Sample: true}}},
		{Items: []SuiteItem{{Prompt: "p", Sample: true}, {Prompt: "q", NoProduct: true, UseSample: true}}},
	} {
		if e := s.validatePlan(&r, &bad); e == nil {
			t.Fatal("no-product image accepted as or with a sample")
		}
	}
}

func TestSkillsFrom0612AreUpgradedToLevelCuffs(t *testing.T) {
	s := testService(t)
	old := map[string]string{
		"etsy-suite":      builtinSkillContent(builtinSkills[1].slug, builtinSkills[1].description, embeddedSkillText("builtin_skills/history/listing-suite-0.6.12.md")),
		p004SkillID:       builtinSkillContent(p004SkillID, builtinSkills[3].description, embeddedSkillText("builtin_skills/history/p004-rules-0.6.12.md")),
		"image-prompting": builtinSkillContent("image-prompting", builtinSkills[4].description, embeddedSkillText("builtin_skills/history/image-prompting-0.6.12.md")),
	}
	for id, content := range old {
		var rec SkillRecord
		s.store.Get("skill", id, &rec)
		rec.Content, rec.Version = content, 7
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
		if rec.Version != 8 || rec.Content != b.content() {
			t.Fatal("0.6.12 copy not upgraded", b.id, rec.Version)
		}
	}
	if projectRulesSuffix(AgentRun{Skills: []SkillRecord{{ID: p004SkillID, Content: old[p004SkillID]}}}, SuitePlan{}) != "\n"+p004FixedRules {
		t.Fatal("0.6.12 p004 copy lost the fixed rules")
	}
	for _, want := range []string{"noProduct", "不放整件衣服的参考图", "只要整件衣服出现在画面里"} {
		if !strings.Contains(builtinSkills[1].body, want) {
			t.Fatal("listing-suite rule missing", want)
		}
	}
}
