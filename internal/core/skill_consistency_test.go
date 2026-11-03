package core

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Exercise persisted upgrades, rather than checking that a Markdown heading or
// keyword exists. Every previously shipped skill must upgrade exactly once,
// preserve user state, and leave already-started runs frozen.
func TestConsistencySkillUpgradePreservesStoredState(t *testing.T) {
	for _, source := range []string{"内置", "用户编辑", "导入"} {
		t.Run(source, func(t *testing.T) {
			s := testService(t)
			frozen := AgentRun{ID: NewID(), Status: "review"}
			before := map[string]SkillRecord{}
			for _, b := range builtinSkills {
				var rec SkillRecord
				if e := s.store.Get("skill", b.id, &rec); e != nil {
					t.Fatal(e)
				}
				rec.Content = builtinSkillContent(b.slug, b.description, embeddedSkillText("builtin_skills/history/"+b.slug+"-0.6.25.md"))
				rec.Version, rec.Enabled = 8, false
				rec.Name = "我的 " + b.name
				rec.Attachments = []SkillAttachment{{Name: "notes.txt", Path: "skills/" + b.id + "/notes.txt"}}
				if source == "用户编辑" {
					rec.Content += "\n保留我自己的设计规则。\n"
				} else if source == "导入" {
					rec.Source = source
				}
				before[b.id] = rec
				frozen.Skills = append(frozen.Skills, rec)
				if e := s.store.Put("skill", rec.ID, rec); e != nil {
					t.Fatal(e)
				}
			}
			if e := s.store.Put("agent_run", frozen.ID, frozen); e != nil {
				t.Fatal(e)
			}
			for pass := 0; pass < 2; pass++ {
				if e := s.initializeAgents(); e != nil {
					t.Fatal(e)
				}
				for _, b := range builtinSkills {
					var got SkillRecord
					if e := s.store.Get("skill", b.id, &got); e != nil {
						t.Fatal(e)
					}
					want := before[b.id]
					if source == "内置" {
						want.Content, want.Description, want.Version = b.content(), b.description, 9
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("%s: upgrade changed unexpected state on pass %d", b.id, pass)
					}
				}
			}
			var gotFrozen AgentRun
			if e := s.store.Get("agent_run", frozen.ID, &gotFrozen); e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(gotFrozen.Skills, frozen.Skills) {
				t.Fatal("upgrade rewrote an existing run's frozen skills")
			}
			// The last shipped P004 copy still receives fixed rules on old runs.
			if source == "内置" && projectRulesSuffix(gotFrozen, SuitePlan{}) != "\n"+p004FixedRules {
				t.Fatal("old frozen P004 copy lost its project rules")
			}
			if source == "用户编辑" && projectRulesSuffix(gotFrozen, SuitePlan{}) != "" {
				t.Fatal("edited P004 copy received unrequested fixed rules")
			}
		})
	}
}

func TestConsistencyPromptCarriesTheRunsActualSkillVersions(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.invalid/anthropic")
	if e := s.store.Get("connection", llm.ID, &llm); e != nil {
		t.Fatal(e)
	}
	for _, legacy := range []bool{false, true} {
		r := AgentRun{ID: NewID(), Status: "review", LLMConnection: llm}
		for _, b := range builtinSkills {
			content := b.content()
			if legacy {
				content = builtinSkillContent(b.slug, b.description, embeddedSkillText("builtin_skills/history/"+b.slug+"-0.6.20.md"))
			}
			r.Skills = append(r.Skills, SkillRecord{ID: b.id, Content: content})
		}
		if e := s.store.Put("agent_run", r.ID, r); e != nil {
			t.Fatal(e)
		}
		called := false
		s.agentRunner = func(ctx context.Context, in agentInvocation) error {
			called = true
			for _, skill := range r.Skills {
				if !strings.Contains(in.Prompt, skillBody(skill.Content)) {
					t.Fatalf("runner received a different skill version: %s", skill.ID)
				}
			}
			emitAgentSuccess(in)
			return nil
		}
		if e := s.executeAgent(context.Background(), r, "核对本次设计规格"); e != nil {
			t.Fatal(e)
		}
		if !called {
			t.Fatal("runner did not start")
		}
	}
}

func TestAgentPromptCarriesTheContextOnce(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.invalid/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	input := agentInput(llm, images)
	var prompt string
	var args []string
	var view AgentRun
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		prompt, args = in.Prompt, in.Args
		view = invocationRun(t, in)
		b := &agentBridge{service: s, runID: view.ID, ctx: ctx}
		out, e := b.callTool("get_run_context", nil)
		if e != nil {
			return e
		}
		if text := out[0].(map[string]any)["text"].(string); len(text) > 400 || strings.Contains(text, "skills") {
			t.Errorf("get_run_context repeats the context: %s", text)
		}
		emitAgentSuccess(in)
		return nil
	}
	id, e := s.StartSmartGeneration(input, "context-once")
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	r, e := s.GetAgentRun(id)
	if e != nil {
		t.Fatal(e)
	}
	if view.ID != id {
		t.Fatal("context has the wrong run", view.ID)
	}
	if strings.Contains(prompt, "get_run_context") || strings.Contains(prompt, "\"events\"") || strings.Contains(prompt, "\"llmConnection\"") || strings.Contains(prompt, "\"imageConnection\"") || strings.Contains(prompt, "imageSources\":{") {
		t.Fatal("prompt carries what the Agent does not need")
	}
	for _, skill := range r.Skills {
		if strings.Count(prompt, skillBody(skill.Content)) != 1 {
			t.Fatal("skill not in the prompt exactly once", skill.ID)
		}
		if strings.Contains(prompt, "description: "+skill.Description) {
			t.Fatal("skill frontmatter sent", skill.ID)
		}
	}
	if !slices.Contains(args, "--disable-slash-commands") || slices.Contains(args, "Skill") {
		t.Fatal("Claude Code's own skills stay enabled", args)
	}
	// Only the latest plan is the existing plan.
	r.Plans = []SuitePlan{{Notes: "旧方案"}, {Notes: "新方案"}}
	if text, _ := json.Marshal(newAgentContext(r)); strings.Contains(string(text), "旧方案") || !strings.Contains(string(text), "新方案") {
		t.Fatal("older plans sent", string(text))
	}
}

func TestShippedSkillsStayConcise(t *testing.T) {
	total := 0
	for _, b := range builtinSkills {
		total += len([]rune(b.body))
		if strings.Contains(b.body, "get_run_context") || strings.Contains(b.body, "fetch_product") {
			t.Fatal("skill still asks to read what the prompt holds", b.id)
		}
	}
	if total > 16000 {
		t.Fatal("skills grew past the size the context budget assumes", total)
	}
	// The pose table saves the Agent working out P004's cuff pose for each image.
	for _, want := range []string{"姿势表", "站姿", "坐姿", "多人同框", "平铺"} {
		if !strings.Contains(builtinSkills[3].body, want) {
			t.Fatal("P004 pose missing", want)
		}
	}
	if !strings.Contains(builtinSkills[1].body, "保存前不要再写长篇分析") {
		t.Fatal("suite skill lost the instruction to save without more analysis")
	}
}
