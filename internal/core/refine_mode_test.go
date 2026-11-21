package core

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"imageworkbench/internal/i18n"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// planFixture gives slot-1 of the refine fixture a submitted plan: its prompt and
// one reference picture of the product, as a real suite has.
func planFixture(t *testing.T, s *Service, first Task) (Task, string) {
	t.Helper()
	ref, e := s.ImportImage("product.png", base64.StdEncoding.EncodeToString(tinyPNG()))
	if e != nil {
		t.Fatal(e)
	}
	planPrompt := "主体：刺绣卫衣上身图，金发女模特站在户外"
	var run AgentRun
	s.store.Get("agent_run", first.AgentRunID, &run)
	run.Plans = []SuitePlan{{Revision: 1, Items: []SuiteItem{{ID: "slot-1", Prompt: planPrompt, References: []string{ref.ID}}, {ID: "slot-2", Prompt: "原始提示词 slot-2"}}}}
	run.SubmittedRevision = 1
	s.store.Put("agent_run", run.ID, run)
	first.Prompt, first.References = planPrompt, []string{ref.ID}
	s.store.Put("task", first.ID, first)
	return first, ref.ID
}

// fakeLLM answers every rewrite with reply.
func fakeLLM(t *testing.T, s *Service, reply string) {
	t.Helper()
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"content": []map[string]string{{"type": "text", "text": reply}}})
	}))
	t.Cleanup(llm.Close)
	if _, e := s.SaveConnection(ConnectionInput{Kind: "llm", Name: "LLM", BaseURL: llm.URL, Model: "m", Key: "k"}); e != nil {
		t.Fatal(e)
	}
}

// Asking for another model regenerates the image from its plan: the old image is
// not the base, the plan's references are, and the prompt is the plan's with the change.
func TestRefineChangingTheModelRegeneratesFromThePlan(t *testing.T) {
	s, first, _, calls, mu := refineFixture(t)
	first, ref := planFixture(t, s, first)
	fakeLLM(t, s, "[regenerate]\n主体：刺绣卫衣上身图，男模特站在户外")
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "r-model", TaskID: first.ID, AssetID: first.AssetIDs[0], Instruction: "换个男模特"}); e != nil {
		t.Fatal(e)
	}
	s.Start(func() {})
	done := waitTask(t, s, first.AgentRunID, "slot-1", func(task Task) bool {
		return task.RefinedFrom == first.ID && (task.Status == "succeeded" || task.Status == "failed")
	})
	if done.Status != "succeeded" || done.RefineMode != "regenerate" || done.RefineUnjudged {
		t.Fatalf("not regenerated: %s %q mode=%q", done.Status, done.Error, done.RefineMode)
	}
	if slices.Contains(done.References, first.AssetIDs[0]) || !slices.Equal(done.References, []string{ref}) {
		t.Fatalf("regeneration must use the plan's references, not the old image: %v", done.References)
	}
	mu.Lock()
	sent := append([]editCall{}, (*calls)...)
	mu.Unlock()
	if len(sent) != 1 || sent[0].images != 1 {
		t.Fatalf("unexpected image requests: %+v", sent)
	}
	p := sent[0].prompt
	if !strings.Contains(p, "男模特站在户外") || strings.Contains(p, "金发女模特") || !strings.Contains(p, "换个男模特") || strings.Contains(p, "底图") {
		t.Fatalf("prompt is not the plan's with the change: %q", p)
	}

	// Same slot: the regenerated image is the current version, the old one is kept.
	tasks, _ := s.GetAgentTasks(first.AgentRunID)
	if len(tasks) != 2 || tasks[0].ID != done.ID || !slices.Equal(tasks[0].PreviousAssetIDs, first.AssetIDs) {
		t.Fatalf("slot versions: %+v", tasks)
	}
	path := filepath.Join(t.TempDir(), "set.zip")
	if n, e := s.ExportBatch("batch-1", path); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	zr, _ := zip.OpenReader(path)
	defer zr.Close()
	f, _ := zr.File[0].Open()
	data, _ := io.ReadAll(f)
	f.Close()
	var current Asset
	s.store.Get("asset", done.AssetIDs[0], &current)
	want, _ := readOwned(s.store.Root, current.Path)
	if string(data) != string(want) {
		t.Fatal("export is not the regenerated version")
	}
}

// A local adjustment still edits the old image, in the same slot.
func TestRefineLocalAdjustmentEditsTheOldImage(t *testing.T) {
	s, first, _, calls, mu := refineFixture(t)
	first, _ = planFixture(t, s, first)
	fakeLLM(t, s, "[edit]\n主体：刺绣卫衣上身图，金发女模特站在户外，画面更亮")
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "r-light", TaskID: first.ID, AssetID: first.AssetIDs[0], Instruction: "亮一点"}); e != nil {
		t.Fatal(e)
	}
	s.Start(func() {})
	done := waitTask(t, s, first.AgentRunID, "slot-1", func(task Task) bool {
		return task.RefinedFrom == first.ID && (task.Status == "succeeded" || task.Status == "failed")
	})
	if done.Status != "succeeded" || done.RefineMode != "edit" || done.RefineUnjudged {
		t.Fatalf("not an edit: %s %q mode=%q unjudged=%v", done.Status, done.Error, done.RefineMode, done.RefineUnjudged)
	}
	if len(done.References) == 0 || done.References[0] != first.AssetIDs[0] {
		t.Fatalf("the old image must be the base: %v", done.References)
	}
	mu.Lock()
	sent := append([]editCall{}, (*calls)...)
	mu.Unlock()
	if len(sent) != 1 || !strings.Contains(sent[0].prompt, "以提供的第 1 张图片为底图修改") || !strings.Contains(sent[0].prompt, "画面更亮") {
		t.Fatalf("unexpected edit request: %+v", sent)
	}
	tasks, _ := s.GetAgentTasks(first.AgentRunID)
	if tasks[0].ID != done.ID || !slices.Equal(tasks[0].PreviousAssetIDs, first.AssetIDs) {
		t.Fatalf("slot versions: %+v", tasks)
	}
}

// When the LLM does not say how, the change is made on the old image and the
// task says it could not be decided.
func TestRefineUndecidedChangeEditsTheOldImageWithANote(t *testing.T) {
	s, first, _, _, _ := refineFixture(t)
	first, _ = planFixture(t, s, first)
	fakeLLM(t, s, "主体：刺绣卫衣上身图，男模特站在户外")
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "r-undecided", TaskID: first.ID, AssetID: first.AssetIDs[0], Instruction: "换个男模特"}); e != nil {
		t.Fatal(e)
	}
	s.Start(func() {})
	done := waitTask(t, s, first.AgentRunID, "slot-1", func(task Task) bool {
		return task.RefinedFrom == first.ID && (task.Status == "succeeded" || task.Status == "failed")
	})
	if done.Status != "succeeded" || done.RefineMode != "edit" || !done.RefineUnjudged || done.References[0] != first.AssetIDs[0] {
		t.Fatalf("undecided change: %s %q mode=%q unjudged=%v refs=%v", done.Status, done.Error, done.RefineMode, done.RefineUnjudged, done.References)
	}
	if !strings.Contains(done.Prompt, "男模特站在户外") {
		t.Fatalf("rewritten requirements lost: %q", done.Prompt)
	}
}

// A regenerated image that was drawn from a design-area sample gets the sample
// first again, with the sample line, and never the old image.
func TestRegeneratedRefinementGetsTheSampleFirst(t *testing.T) {
	s, runID, calls, mu := sampleFixture(t, false)
	sample := waitTask(t, s, runID, "sample", func(task Task) bool { return task.Status == "succeeded" })
	uses := waitTask(t, s, runID, "uses", func(task Task) bool { return task.Status == "succeeded" })
	fakeLLM(t, s, "[regenerate]\n画面 SLOT:uses 男模特坐在室内")
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "r-person", TaskID: uses.ID, AssetID: uses.AssetIDs[0], Instruction: "换个人，坐在室内"}); e != nil {
		t.Fatal(e)
	}
	done := waitTask(t, s, runID, "uses", func(task Task) bool {
		return task.RefinedFrom == uses.ID && (task.Status == "succeeded" || task.Status == "failed")
	})
	if done.Status != "succeeded" || done.RefineMode != "regenerate" || !slices.Equal(done.References, sample.AssetIDs) || done.SampleAssetID != sample.AssetIDs[0] {
		t.Fatalf("regeneration without its sample: %s %q %+v", done.Status, done.Error, done)
	}
	mu.Lock()
	call := *calls["SLOT:uses"]
	mu.Unlock()
	if call.images != 1 || !strings.HasPrefix(call.prompt, samplePromptLine(1, i18n.ZhCN)) || !strings.Contains(call.prompt, "换个人，坐在室内") || strings.Contains(call.prompt, refineSampleLine) {
		t.Fatalf("unexpected regeneration request: %d %q", call.images, call.prompt)
	}
}

// A regenerated refinement's wrapper follows the prompt language and is taken
// off again by the next refinement, so prompts do not nest.
func TestRegeneratedPromptStripsCleanlyInBothLanguages(t *testing.T) {
	for _, lang := range []string{i18n.ZhCN, i18n.En} {
		p := regeneratePrompt("A hoodie", "another model", lang)
		if baseTaskPrompt(Task{RefinedFrom: "x", Prompt: p}) != "A hoodie" || (lang == i18n.En && hasHan(p)) {
			t.Fatalf("%s: %q", lang, p)
		}
	}
}
