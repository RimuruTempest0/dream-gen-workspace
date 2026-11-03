package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestReplyFrameCountsTextAndFindsThePlan(t *testing.T) {
	frame := func(v string) []byte { return []byte("event: x\ndata: " + v + "\n\n") }
	cases := []struct {
		frame string
		chars int
		plan  bool
	}{
		{`{"type":"content_block_delta","delta":{"type":"text_delta","text":"套图方案"}}`, 4, false},
		{`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"abc"}}`, 3, false},
		{`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"a"}}`, 3, false},
		{`{"type":"content_block_start","content_block":{"type":"tool_use","name":"mcp__workbench__save_suite_plan"}}`, 0, true},
		{`{"type":"content_block_start","content_block":{"type":"tool_use","name":"mcp__workbench__fetch_product"}}`, 0, false},
		{`not json`, 0, false},
	}
	for _, c := range cases {
		if got := readReplyFrame(frame(c.frame)); got.chars != c.chars || got.writingPlan != c.plan {
			t.Errorf("%s: got %+v", c.frame, got)
		}
	}
}

func TestLiveReplyIsAddedOnReadAndNeverSaved(t *testing.T) {
	s := testService(t)
	r := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "analyzing", Progress: newRunProgress()}
	s.store.Put("agent_run", r.ID, r)
	s.replyStart(r.ID)
	s.replyAdd(r.ID, 1200, true)
	s.replyAdd(r.ID, 34, true)
	got, e := s.GetAgentRun(r.ID)
	if e != nil || !got.Progress.ReplyActive || !got.Progress.ReplyThinking || got.Progress.ReplyChars != 1234 || got.Progress.ReplyLastAt == "" {
		t.Fatalf("%+v %v", got.Progress, e)
	}
	var saved AgentRun
	s.store.Get("agent_run", r.ID, &saved)
	if saved.Progress.ReplyChars != 0 || saved.Progress.ReplyActive {
		t.Fatalf("reply was saved: %+v", saved.Progress)
	}
	s.replyEnd(r.ID)
	if got, _ = s.GetAgentRun(r.ID); got.Progress.ReplyActive {
		t.Fatal("reply still active after the request ended")
	}
}

func TestProgressFollowsReferencesAndPlanBatches(t *testing.T) {
	s := testService(t)
	asset, e := s.ImportImage("a.png", base64.StdEncoding.EncodeToString(tinyPNG()))
	if e != nil {
		t.Fatal(e)
	}
	r := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "analyzing", Progress: newRunProgress(), Input: AgentInput{TargetCount: 12, References: []string{asset.ID}}}
	r.LLMConnection.Capabilities.Vision = true
	s.store.Put("agent_run", r.ID, r)
	b := &agentBridge{service: s, runID: r.ID, ctx: context.Background()}
	progress := func() *RunProgress {
		var v AgentRun
		s.store.Get("agent_run", r.ID, &v)
		return v.Progress
	}
	look, _ := json.Marshal(map[string]string{"assetID": asset.ID})
	for i := 0; i < 2; i++ { // the same image twice counts once
		if _, e = b.callTool("read_reference_image", look); e != nil {
			t.Fatal(e)
		}
	}
	if p := progress(); p.Step != stepReferences || p.ReferencesSeen != 1 {
		t.Fatalf("%+v", p)
	}
	started := progress().StepStartedAt
	if _, e = b.callTool("read_reference_image", look); e != nil || progress().StepStartedAt != started {
		t.Fatal("staying on a step restarted its timer", e)
	}
	full := simplePlan(12, []string{})
	first := SuitePlan{Locked: full.Locked, Editable: full.Editable, Items: full.Items[:6]}
	if _, e = b.callTool("save_suite_plan", planArgs(t, first, true)); e != nil {
		t.Fatal(e)
	}
	if p := progress(); p.Step != stepPlan || p.PlanWritten != 6 {
		t.Fatalf("%+v", p)
	}
	if _, e = b.callTool("save_suite_plan", planArgs(t, SuitePlan{Items: full.Items[6:]}, false)); e != nil {
		t.Fatal(e)
	}
	p := progress()
	if p.Step != stepSubmit || p.PlanWritten != 12 || !slices.Contains(p.Visited, stepReferences) || !slices.Contains(p.Visited, stepPlan) {
		t.Fatalf("%+v", p)
	}
	// Going back to an earlier step is shown as it happens.
	if _, e = b.callTool("read_reference_image", look); e != nil || progress().Step != stepReferences {
		t.Fatal("earlier step did not become current", e)
	}
}

func TestProgressRecordsTheProductRead(t *testing.T) {
	server := productServer(t)
	s := fakeBrowserService(t, "page", productPage(server.URL))
	r := productRun(t, s, server.URL+"/listing")
	if _, e := s.fetchProduct(context.Background(), r.ID); e != nil {
		t.Fatal(e)
	}
	var saved AgentRun
	s.store.Get("agent_run", r.ID, &saved)
	p := saved.Progress
	if p == nil || p.ProductVia != "browser" || p.ImagesFound != 2 || p.ImagesSaved != 2 || p.Step != stepImages {
		t.Fatalf("%+v", p)
	}
	if !slices.Contains(p.Visited, stepProduct) || !slices.Contains(p.Visited, stepImages) {
		t.Fatalf("%+v", p)
	}
}

// Progress is for the customer; the planning Agent must not receive it.
func TestProgressNeverReachesTheAgent(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		if strings.Contains(in.Prompt, "stepStartedAt") {
			return errors.New("progress in the Agent's context")
		}
		emitAgentSuccess(in)
		return nil
	}
	id, e := s.StartSmartGeneration(agentInput(llm, images), "progress-agent")
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	r, _ := s.GetAgentRun(id)
	if strings.Contains(r.Error, "progress") {
		t.Fatal(r.Error)
	}
	if r.Progress == nil {
		t.Fatal("run has no progress")
	}
	b := &agentBridge{service: s, runID: id, ctx: context.Background()}
	out, e := b.callTool("get_run_context", nil)
	if e != nil || strings.Contains(out[0].(map[string]any)["text"].(string), "stepStartedAt") {
		t.Fatal("progress in get_run_context", e)
	}
}
