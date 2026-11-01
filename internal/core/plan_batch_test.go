package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func planArgs(t *testing.T, p SuitePlan, appendBatch bool) json.RawMessage {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"locked": p.Locked, "editable": p.Editable, "notes": p.Notes, "items": p.Items, "append": appendBatch})
	return b
}

func TestSuitePlanCanBeSavedInBatches(t *testing.T) {
	s := testService(t)
	r := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "analyzing", Input: AgentInput{TargetCount: 12, References: []string{}}}
	s.store.Put("agent_run", r.ID, r)
	b := &agentBridge{service: s, runID: r.ID, ctx: context.Background()}
	full := simplePlan(12, []string{})
	first := SuitePlan{Locked: full.Locked, Editable: full.Editable, Items: full.Items[:6]}
	out, e := b.callTool("save_suite_plan", planArgs(t, first, true))
	if e != nil || !strings.Contains(out[0].(map[string]any)["text"].(string), `"staged":6`) {
		t.Fatal("first batch not staged", out, e)
	}
	if saved, _ := s.GetAgentRun(r.ID); len(saved.Plans) != 0 {
		t.Fatal("a staged batch must not be saved as a plan")
	}
	tooBig := SuitePlan{Items: simplePlan(7, []string{}).Items}
	if _, e = b.callTool("save_suite_plan", planArgs(t, tooBig, true)); e == nil {
		t.Fatal("batch larger than 6 accepted")
	}
	last := SuitePlan{Items: full.Items[6:]}
	if _, e = b.callTool("save_suite_plan", planArgs(t, last, false)); e != nil {
		t.Fatal(e)
	}
	saved, _ := s.GetAgentRun(r.ID)
	if len(saved.Plans) != 1 || len(saved.Plans[0].Items) != 12 || saved.Plans[0].Locked[0] != "比例" {
		t.Fatalf("batches not merged: %+v", saved.Plans)
	}

	// An incomplete final save clears the staging so a retry cannot mix old and new items.
	if _, e = b.callTool("save_suite_plan", planArgs(t, SuitePlan{Locked: full.Locked, Editable: full.Editable, Items: full.Items[:3]}, true)); e != nil {
		t.Fatal(e)
	}
	if _, e = b.callTool("save_suite_plan", planArgs(t, SuitePlan{Items: full.Items[:2]}, false)); e == nil || !strings.Contains(e.Error(), "已清空") {
		t.Fatal("incomplete plan accepted or staging not reported", e)
	}
	if _, e = b.callTool("save_suite_plan", planArgs(t, SuitePlan{Items: full.Items}, false)); e == nil || !strings.Contains(e.Error(), "locked") {
		t.Fatal("staging survived a failed save or plan without locked accepted", e)
	}
	// A single complete call keeps working as before.
	if _, e = b.callTool("save_suite_plan", planArgs(t, full, false)); e != nil {
		t.Fatal(e)
	}
}

func TestOutputLimitIsReportedInsteadOfFollowUpError(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		in.Event([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"API Error: response exceeded the 8192 output token maximum."}]},"error":"max_output_tokens"}`))
		return errors.New(`Claude Code 执行失败：[claude-code:unrecognized_model] {"model":"deepseek-flash"}`)
	}
	id, e := s.StartAgentAnalysis(agentInput(llm, images))
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	r, _ := s.GetAgentRun(id)
	if !strings.Contains(r.Error, "单次输出超过上限（32768 tokens）") || strings.Contains(r.Error, "unrecognized_model") {
		t.Fatal("real cause hidden", r.Error)
	}

	// A customer cannot edit a line from customer.dat, so the hint points to the provider.
	s.mu.Lock()
	var managed Connection
	s.store.Get("connection", llm.ID, &managed)
	managed.Managed = "llm-1"
	s.store.Put("connection", llm.ID, managed)
	s.mu.Unlock()
	id, e = s.StartAgentAnalysis(agentInput(llm, images))
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	r, _ = s.GetAgentRun(id)
	if !strings.Contains(r.Error, "单次输出超过上限（32768 tokens）") || !strings.Contains(r.Error, "请联系软件提供方") || strings.Contains(r.Error, "连接与设置") {
		t.Fatal("customer told to edit a locked line", r.Error)
	}
}

func TestSkillCopiedFrom033IsUpgraded(t *testing.T) {
	s := testService(t)
	var suite SkillRecord
	s.store.Get("skill", "etsy-suite", &suite)
	suite.Content = shippedContent("etsy-suite", embeddedSkillText("builtin_skills/history/etsy-suite-0.3.3.md"))
	suite.Version = 4
	s.store.Put("skill", suite.ID, suite)
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	s.store.Get("skill", "etsy-suite", &suite)
	if suite.Version != 5 || !strings.Contains(suite.Content, "append: true") {
		t.Fatal("0.3.3 skill copy not upgraded", suite.Version)
	}
}

func TestSkillsFrom034AreUpgraded(t *testing.T) {
	s := testService(t)
	old := map[string]string{
		"product-analysis": "builtin_skills/history/product-analysis-0.3.3.md",
		"etsy-suite":       "builtin_skills/history/etsy-suite-0.3.4.md",
		"embroidery-rules": "builtin_skills/history/embroidery-rules-0.3.3.md",
	}
	for _, b := range builtinSkills[:3] {
		var rec SkillRecord
		s.store.Get("skill", b.id, &rec)
		rec.Content = shippedContent(b.id, embeddedSkillText(old[b.id]))
		rec.Version = 7
		s.store.Put("skill", rec.ID, rec)
	}
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	for _, b := range builtinSkills[:3] {
		var rec SkillRecord
		s.store.Get("skill", b.id, &rec)
		if rec.Version != 8 || rec.Content != b.content() {
			t.Fatal("0.3.4 copy not upgraded", b.id, rec.Version)
		}
	}
	// The current texts state one direction phrasing and never the contradictory mix.
	for _, b := range builtinSkills {
		if strings.Contains(b.body, "沿袖口方向竖排") {
			t.Fatal("contradictory direction wording in", b.id)
		}
	}
	if !strings.Contains(builtinSkills[0].body, "文字沿袖子长度方向排列") || !strings.Contains(builtinSkills[1].body, "不要额外添加细节") {
		t.Fatal("new direction or panel rules missing")
	}
}

func TestOldDefaultOutputLimitIsRaised(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	// Saved by a version before 0.6.10 (or a customer package without a limit).
	s.mu.Lock()
	var stored Connection
	s.store.Get("connection", llm.ID, &stored)
	stored.MaxOutputTokens = 8192
	s.store.Put("connection", llm.ID, stored)
	s.mu.Unlock()
	if got := normalizeConnection(stored).MaxOutputTokens; got != DefaultMaxOutputTokens {
		t.Fatal("old default kept", got)
	}
	cs, _ := s.ListConnections()
	for _, c := range cs {
		if c.ID == llm.ID && c.MaxOutputTokens != DefaultMaxOutputTokens {
			t.Fatal("settings still show the old default", c.MaxOutputTokens)
		}
	}
	// Saving an empty or old-default value stores 32768; a deliberate other value is kept.
	for in, want := range map[int]int{0: 32768, 8192: 32768, 16000: 16000, 65536: 65536} {
		saved, e := s.SaveConnection(ConnectionInput{ID: llm.ID, Kind: "llm", Name: "LLM", BaseURL: "https://example.com/anthropic", Model: "m", MaxOutputTokens: in})
		if e != nil || saved.MaxOutputTokens != want {
			t.Fatal(in, saved.MaxOutputTokens, e)
		}
	}
}
