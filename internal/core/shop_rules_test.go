package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShopRulesAreValidatedAndHoldAttachments(t *testing.T) {
	s := testService(t)
	if _, e := s.SaveShopRule(ShopRule{Name: "", Text: "x"}); e == nil {
		t.Fatal("rule without name saved")
	}
	if _, e := s.SaveShopRule(ShopRule{Name: "规则", Text: "  "}); e == nil {
		t.Fatal("empty rule saved")
	}
	rule, e := s.SaveShopRule(ShopRule{Name: "本店规则", Text: "模特只用欧美人。\r\n刺绣不超过 25 cm。", Enabled: true})
	if e != nil || rule.Version != 1 || strings.Contains(rule.Text, "\r") {
		t.Fatal(rule, e)
	}
	dir := t.TempDir()
	png := filepath.Join(dir, "色卡.png")
	os.WriteFile(png, tinyPNG(), 0o600)
	exe := filepath.Join(dir, "tool.exe")
	os.WriteFile(exe, []byte("MZ"), 0o600)
	if _, e = s.AddShopRuleFiles(rule.ID, []string{exe}); e == nil {
		t.Fatal("executable attached")
	}
	rule, e = s.AddShopRuleFiles(rule.ID, []string{png})
	if e != nil || len(rule.Attachments) != 1 || !strings.HasPrefix(rule.Attachments[0].Path, "skills/shop-rules/") {
		t.Fatal(rule, e)
	}
	if data, e := s.readSkillAttachment(rule.Attachments[0].Path); e != nil || len(data) == 0 {
		t.Fatal("attachment not readable", e)
	}
	if rule, e = s.RemoveShopRuleFile(rule.ID, "色卡.png"); e != nil || len(rule.Attachments) != 0 {
		t.Fatal(rule, e)
	}
}

func TestRunsFreezeShopRulesAndPickSkillsByCategory(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	rule, _ := s.SaveShopRule(ShopRule{Name: "本店规则", Text: "所有图片都用欧美模特。", Enabled: true})
	s.SaveShopRule(ShopRule{Name: "停用的规则", Text: "不应出现的规则文字", Enabled: false})
	png := filepath.Join(t.TempDir(), "姿势.png")
	os.WriteFile(png, tinyPNG(), 0o600)
	rule, _ = s.AddShopRuleFiles(rule.ID, []string{png})
	mug, e := s.SaveSkill(SkillRecord{Name: "马克杯", Content: "---\nname: mug-rules\ndescription: 马克杯套图\n---\n\n马克杯专用规则", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetSkillCategory(mug.ID, "马克杯"); e != nil {
		t.Fatal(e)
	}
	if cats, _ := s.SkillCategories(); len(cats) != 1 || cats[0] != "马克杯" {
		t.Fatal("categories", cats)
	}
	check := func(category string, wantMug bool) {
		t.Helper()
		var prompt string
		var run AgentRun
		s.agentRunner = func(ctx context.Context, in agentInvocation) error {
			prompt, run = in.Prompt, invocationRun(t, in)
			b := &agentBridge{service: s, runID: run.ID, ctx: context.Background()}
			args, _ := json.Marshal(map[string]string{"skillID": rule.ID, "name": "姿势.png"})
			if _, e := b.callTool("read_skill_attachment", args); e != nil {
				return errors.New("shop rule attachment: " + e.Error())
			}
			emitAgentSuccess(in)
			return nil
		}
		in := agentInput(llm, images)
		in.Category = category
		id, e := s.StartSmartGeneration(in, "shop-rules-"+category)
		if e != nil {
			t.Fatal(e)
		}
		s.waitAgent()
		r, _ := s.GetAgentRun(id)
		if strings.Contains(r.Error, "shop rule attachment") {
			t.Fatal(r.Error)
		}
		rules := strings.Index(prompt, "本次启用的店铺规则")
		skills := strings.Index(prompt, "本次选定的技能")
		if rules < 0 || skills < 0 || rules > skills || !strings.Contains(prompt, "所有图片都用欧美模特。") || strings.Contains(prompt, "不应出现的规则文字") {
			t.Fatalf("shop rules not ahead of the skills in the prompt")
		}
		var stored AgentRun
		s.store.Get("agent_run", id, &stored)
		if !strings.Contains(prompt, "启用了店铺规则时") || len(stored.ShopRules) != 1 {
			t.Fatal("shop rules not frozen or prompt wording missing")
		}
		if strings.Contains(prompt, "马克杯专用规则") != wantMug {
			t.Fatalf("category %q: mug skill used = %v", category, !wantMug)
		}
	}
	check("", false)
	check("马克杯", true)
	// Editing the rule later does not change a frozen run.
	if _, e = s.SaveShopRule(ShopRule{ID: rule.ID, Name: "本店规则", Text: "改过的规则", Enabled: true}); e != nil {
		t.Fatal(e)
	}
}
