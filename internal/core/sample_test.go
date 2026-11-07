package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type sampleCall struct {
	prompt      string
	images      int
	start, done time.Time
}

// sampleFixture queues a detail sample, an image drawn from it and an unrelated
// image in one batch. The sample request is slow so a missing wait would show.
func sampleFixture(t *testing.T, sampleFails bool) (*Service, string, map[string]*sampleCall, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	calls := map[string]*sampleCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := &sampleCall{start: time.Now()}
		if r.URL.Path == "/images/edits" {
			r.ParseMultipartForm(8 << 20)
			call.prompt = r.FormValue("prompt")
			call.images = len(r.MultipartForm.File["image"]) + len(r.MultipartForm.File["image[]"])
		} else {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			call.prompt, _ = body["prompt"].(string)
		}
		slot := strings.Fields(call.prompt[strings.LastIndex(call.prompt, "SLOT:"):])[0]
		if slot == "SLOT:sample" {
			time.Sleep(400 * time.Millisecond)
		}
		call.done = time.Now()
		mu.Lock()
		calls[slot] = call
		mu.Unlock()
		if slot == "SLOT:sample" && sampleFails {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"message":"bad request"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG())}}})
	}))
	t.Cleanup(server.Close)
	s := testService(t)
	saved := saveConnection(t, s, server.URL)
	var c Connection
	s.store.Get("connection", saved.ID, &c)
	run := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "submitted", BatchID: "batch-s"}
	s.store.Put("agent_run", run.ID, run)
	for _, task := range []Task{
		{PlanItemID: "sample", Sample: true},
		{PlanItemID: "uses", SampleItem: "sample", SamplePending: true},
		{PlanItemID: "other"},
	} {
		task.ID, task.BatchID, task.AgentRunID, task.CreatedAt, task.UpdatedAt = NewID(), "batch-s", run.ID, Now(), Now()
		task.Status, task.Prompt, task.Model, task.Size, task.Connection = "queued", "画面 SLOT:"+task.PlanItemID, "test-image", "1024x1024", c
		task.References, task.AssetIDs = []string{}, []string{}
		s.store.Put("task", task.ID, task)
	}
	s.Start(func() {})
	return s, run.ID, calls, &mu
}

func TestImagesWaitForTheDetailSampleAndGetItAsReference(t *testing.T) {
	s, runID, calls, mu := sampleFixture(t, false)
	sample := waitTask(t, s, runID, "sample", func(task Task) bool { return task.Status == "succeeded" })
	uses := waitTask(t, s, runID, "uses", func(task Task) bool { return task.Status == "succeeded" })
	waitTask(t, s, runID, "other", func(task Task) bool { return task.Status == "succeeded" })
	mu.Lock()
	defer mu.Unlock()
	if calls["SLOT:uses"].start.Before(calls["SLOT:sample"].done) {
		t.Fatal("image drawn from the sample was sent before the sample finished")
	}
	if !calls["SLOT:other"].start.Before(calls["SLOT:sample"].done) {
		t.Fatal("an unrelated image waited for the sample")
	}
	if calls["SLOT:uses"].images != 1 || !strings.HasPrefix(calls["SLOT:uses"].prompt, samplePromptLine(1)) {
		t.Fatalf("sample not attached: %+v", calls["SLOT:uses"])
	}
	if uses.SamplePending || uses.SampleAssetID != sample.AssetIDs[0] || len(uses.References) != 1 || uses.References[0] != sample.AssetIDs[0] {
		t.Fatalf("sample not recorded on the task: %+v", uses)
	}
	if calls["SLOT:sample"].images != 0 {
		t.Fatal("the sample itself must not get a sample reference")
	}
}

func TestImagesGoAheadWithoutAFailedSample(t *testing.T) {
	s, runID, calls, mu := sampleFixture(t, true)
	waitTask(t, s, runID, "sample", func(task Task) bool { return task.Status == "failed" })
	uses := waitTask(t, s, runID, "uses", func(task Task) bool { return task.Status == "succeeded" })
	mu.Lock()
	defer mu.Unlock()
	if calls["SLOT:uses"].images != 0 || strings.Contains(calls["SLOT:uses"].prompt, "样板图") || uses.SampleAssetID != "" || uses.SamplePending {
		t.Fatalf("image should go ahead unchanged after a failed sample: %+v %+v", calls["SLOT:uses"], uses)
	}
}

func TestPlanSampleRules(t *testing.T) {
	s := testService(t)
	r := AgentRun{Input: AgentInput{References: []string{}}}
	item := func(sample, use bool, refs ...string) SuiteItem {
		return SuiteItem{Prompt: "p", References: append([]string{}, refs...), Sample: sample, UseSample: use}
	}
	for name, p := range map[string]SuitePlan{
		"two samples":      {Items: []SuiteItem{item(true, false), item(true, false)}},
		"sample uses self": {Items: []SuiteItem{item(true, true)}},
		"use without one":  {Items: []SuiteItem{item(false, true)}},
		"no room for it":   {Items: []SuiteItem{item(true, false), item(false, true, "a", "b", "c", "d")}},
	} {
		if e := s.validatePlan(&r, &p); e == nil {
			t.Fatal("plan accepted:", name)
		}
	}
	ok := SuitePlan{Items: []SuiteItem{item(true, false), item(false, true), item(false, false)}}
	if e := s.validatePlan(&r, &ok); e != nil {
		t.Fatal(e)
	}
}

func TestSubmittedSuiteLinksImagesToTheSample(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		r := invocationRun(t, in)
		p := simplePlan(12, []string{})
		p.Items[8].Sample = true
		p.Items[2].UseSample, p.Items[3].UseSample = true, true
		if e := saveRunnerPlan(s, r, p); e != nil {
			return e
		}
		emitAgentSuccess(in)
		return nil
	}
	id, e := s.StartSmartGeneration(agentInput(llm, images), "sample-set")
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	tasks, _ := s.GetAgentTasks(id)
	if len(tasks) != 12 || !tasks[8].Sample || tasks[2].SampleItem != tasks[8].PlanItemID || !tasks[3].SamplePending || tasks[4].SamplePending || tasks[8].SamplePending {
		t.Fatalf("sample links wrong: %+v", tasks)
	}
	// The rest of the set copies the sample: only it gets the best quality.
	if tasks[8].Quality != sampleQuality || tasks[2].Quality != "auto" {
		t.Fatal("sample quality", tasks[8].Quality, tasks[2].Quality)
	}
}

func TestSkillsFrom0611LearnTheDetailSample(t *testing.T) {
	s := testService(t)
	old := map[string]string{
		"etsy-suite":      builtinSkillContent(builtinSkills[1].slug, builtinSkills[1].description, embeddedSkillText("builtin_skills/history/listing-suite-0.6.11.md")),
		"image-prompting": builtinSkillContent("image-prompting", builtinSkills[4].description, embeddedSkillText("builtin_skills/history/image-prompting-0.6.11.md")),
	}
	for id, content := range old {
		var rec SkillRecord
		s.store.Get("skill", id, &rec)
		rec.Content, rec.Version = content, 5
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
		if rec.Version != 6 || rec.Content != b.content() || !strings.Contains(b.body, "useSamples") || !strings.Contains(b.body, "sampleArea") {
			t.Fatal("0.6.11 copy not upgraded to the sample rules", b.id, rec.Version)
		}
	}
}

// A refined image that was drawn from the sample sends the sample along as the
// second image, and its prompt no longer points at a reference that is not sent.
func TestRefineSendsTheSampleAlong(t *testing.T) {
	s, runID, calls, mu := sampleFixture(t, false)
	sample := waitTask(t, s, runID, "sample", func(task Task) bool { return task.Status == "succeeded" })
	uses := waitTask(t, s, runID, "uses", func(task Task) bool { return task.Status == "succeeded" })
	other := waitTask(t, s, runID, "other", func(task Task) bool { return task.Status == "succeeded" })
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "r-uses", TaskID: uses.ID, AssetID: uses.AssetIDs[0], Instruction: "把袖口文字摆正"}); e != nil {
		t.Fatal(e)
	}
	refined := waitTask(t, s, runID, "uses", func(task Task) bool { return task.Status == "succeeded" && task.RefinedFrom == uses.ID })
	mu.Lock()
	call := *calls["SLOT:uses"]
	mu.Unlock()
	if call.images != 2 || len(refined.References) != 2 || refined.References[0] != uses.AssetIDs[0] || refined.References[1] != sample.AssetIDs[0] || refined.SampleAssetID != sample.AssetIDs[0] {
		t.Fatalf("sample not sent with the refine: %d %+v", call.images, refined)
	}
	if strings.Contains(call.prompt, samplePromptLine(1)) || !strings.Contains(call.prompt, refineSampleLine) || !strings.Contains(call.prompt, "手臂和袖子改成在画面里竖直下垂") {
		t.Fatal("refine prompt still names the old sample position", call.prompt)
	}
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "r-other", TaskID: other.ID, AssetID: other.AssetIDs[0], Instruction: "背景更亮"}); e != nil {
		t.Fatal(e)
	}
	plain := waitTask(t, s, runID, "other", func(task Task) bool { return task.Status == "succeeded" && task.RefinedFrom == other.ID })
	mu.Lock()
	call = *calls["SLOT:other"]
	mu.Unlock()
	if call.images != 1 || strings.Contains(call.prompt, refineSampleLine) || plain.SampleAssetID != "" || plain.SampleItem != "" {
		t.Fatalf("an image without the sample got one: %d %+v", call.images, plain)
	}
}
