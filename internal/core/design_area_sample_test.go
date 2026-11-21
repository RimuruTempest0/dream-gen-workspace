package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"imageworkbench/internal/i18n"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A sample locks only the design inside its design area (#22): the image model
// is told to copy that design and not the sample's composition, pose,
// presentation or scene, so a close-up sample cannot pull every image into one
// framing.
var sampleOnlyDesign = map[string][]string{
	i18n.ZhCN: {"只照设计区里的设计", "不照样板的构图、姿势、展示方式和场景"},
	i18n.En:   {"Copy only the design inside the design area", "not the sample's composition, pose, presentation or scene"},
}

func assertOnlyDesign(t *testing.T, lang, what, line string) {
	t.Helper()
	for _, want := range sampleOnlyDesign[lang] {
		if !strings.Contains(line, want) {
			t.Errorf("%s (%s) lacks %q: %q", what, lang, want, line)
		}
	}
}

// The main design area's sample is a close-up of the chest design, not a
// whole-garment front view; the plan saves, the set runs, and the image drawn
// from the samples gets them first with a line that locks only the design.
func TestCloseUpMainAreaSampleSavesAndRuns(t *testing.T) {
	var mu sync.Mutex
	sent := map[string]sampleCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := sampleCall{}
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
		mu.Lock()
		sent[slot] = call
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG())}}})
	}))
	t.Cleanup(server.Close)
	// The sample check cannot reach an AI model, so the set goes on without it.
	noLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 500) }))
	t.Cleanup(noLLM.Close)
	s := testService(t)
	llm := saveLLM(t, s, noLLM.URL)
	images := saveConnection(t, s, server.URL)
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		r := invocationRun(t, in)
		p := simplePlan(12, []string{})
		for i := range p.Items {
			p.Items[i].Prompt = fmt.Sprintf("女士室内上身图 SLOT:other-%d", i)
		}
		p.Items[0].Sample, p.Items[0].SampleArea = true, "胸前"
		p.Items[0].Prompt = "胸前图案的局部近景特写，只拍胸前这一块，不画整件衣服 SLOT:chest"
		p.Items[1].Sample, p.Items[1].SampleArea = true, "袖口"
		p.Items[1].Prompt = "袖口局部近景 SLOT:cuff"
		p.Items[2].UseSamples = []string{"胸前", "袖口"}
		p.Items[2].Prompt = "男士户外坐姿上身图 SLOT:wear"
		if e := saveRunnerPlan(s, r, p); e != nil {
			return e
		}
		emitAgentSuccess(in)
		return nil
	}
	id, e := s.StartSmartGeneration(agentInput(llm, images), "close-up-sample")
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	tasks, _ := s.GetAgentTasks(id)
	if len(tasks) != 12 {
		r, _ := s.GetAgentRun(id)
		t.Fatal("suite not submitted", len(tasks), r.Error)
	}
	s.Start(func() {})
	chest := waitTask(t, s, id, tasks[0].PlanItemID, func(task Task) bool { return task.Status == "succeeded" })
	cuff := waitTask(t, s, id, tasks[1].PlanItemID, func(task Task) bool { return task.Status == "succeeded" })
	wear := waitTask(t, s, id, tasks[2].PlanItemID, func(task Task) bool { return task.Status == "succeeded" })
	// The chest sample is drawn as the close-up its prompt asks for.
	if strings.Contains(chest.Prompt, "母版") || strings.Contains(chest.Prompt, "整件正面") || !strings.Contains(chest.Prompt, "胸前图案的局部近景特写") {
		t.Fatalf("chest sample prompt: %q", chest.Prompt)
	}
	if fmt.Sprint(wear.References) != fmt.Sprint([]string{chest.AssetIDs[0], cuff.AssetIDs[0]}) {
		t.Fatalf("samples are not the first references: %v", wear.References)
	}
	mu.Lock()
	call := sent["SLOT:wear"]
	mu.Unlock()
	if call.images != 2 || !strings.HasPrefix(call.prompt, samplesPromptLine([]string{"胸前", "袖口"}, 2, i18n.ZhCN)) {
		t.Fatalf("sample line not sent first: %d images, %q", call.images, call.prompt)
	}
	assertOnlyDesign(t, i18n.ZhCN, "sample line sent", call.prompt)
}

// Every sample line says the same in each prompt language: the one-sample line
// of older plans, the per-area line, and both refinement lines.
func TestSampleLinesLockOnlyTheDesignArea(t *testing.T) {
	for _, lang := range []string{i18n.ZhCN, i18n.En} {
		assertOnlyDesign(t, lang, "one sample, alone", samplePromptLine(1, lang))
		assertOnlyDesign(t, lang, "one sample, with others", samplePromptLine(3, lang))
		assertOnlyDesign(t, lang, "area samples", samplesPromptLine([]string{"胸前", "袖口"}, 3, lang))
		assertOnlyDesign(t, lang, "refine with one sample", refinePrompt("原始提示词", "背景更亮", 1, lang))
		assertOnlyDesign(t, lang, "refine with samples", refinePrompt("原始提示词", "背景更亮", 2, lang))
		if lead := sampleLead("胸前", lang); strings.Contains(lead, "母版") || strings.Contains(strings.ToLower(lead), "master") {
			t.Errorf("main area sample is still a master (%s): %q", lang, lead)
		}
	}
}

// The shipped skills, the planning prompt and the tool schema no longer ask for
// a whole-garment master of the main design area.
func TestNoMasterSampleIsRequired(t *testing.T) {
	for _, b := range builtinSkills {
		if strings.Contains(b.body, "母版") || strings.Contains(b.body, "approved master") {
			t.Errorf("%s still asks for a master", b.id)
		}
	}
	suite := ""
	for _, b := range builtinSkills {
		if b.id == "etsy-suite" {
			suite = b.body
		}
	}
	for _, want := range []string{"样板只锁定设计区里的设计", "优先用 crop_reference 截出这一区的局部"} {
		if !strings.Contains(suite, want) {
			t.Errorf("suite skill lacks %q", want)
		}
	}
	schema, _ := json.Marshal(agentTools())
	if strings.Contains(string(schema), "母版") {
		t.Error("tool schema still asks for a master")
	}
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	var prompt string
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		prompt = in.Prompt
		return nil
	}
	if _, e := s.StartSmartGeneration(agentInput(llm, images), "no-master"); e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	if prompt == "" || strings.Contains(prompt, "母版") {
		t.Error("planning prompt still mentions a master")
	}
}
