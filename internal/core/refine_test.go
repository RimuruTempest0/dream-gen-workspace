package core

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type editCall struct {
	prompt string
	images int
}

func refineFixture(t *testing.T) (*Service, Task, Task, *[]editCall, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	calls := []editCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" || r.ParseMultipartForm(8<<20) != nil {
			t.Error("refine must be a multipart edit", r.URL.Path)
		}
		mu.Lock()
		calls = append(calls, editCall{r.FormValue("prompt"), len(r.MultipartForm.File["image"]) + len(r.MultipartForm.File["image[]"])})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG())}}})
	}))
	t.Cleanup(server.Close)
	s := testService(t)
	c := saveConnection(t, s, server.URL)
	run := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "submitted", BatchID: "batch-1"}
	s.store.Put("agent_run", run.ID, run)
	made := []Task{}
	for _, slot := range []string{"slot-1", "slot-2"} {
		task := Task{ID: NewID(), BatchID: "batch-1", AgentRunID: run.ID, PlanItemID: slot, CreatedAt: Now(), UpdatedAt: Now(), Status: "succeeded", Prompt: "原始提示词 " + slot, Model: "test-image", Size: "1024x1024", Connection: c, References: []string{}}
		s.mu.Lock()
		asset, e := s.saveImage(tinyPNG(), slot+".png", "generated", task.ID, "", "")
		s.mu.Unlock()
		if e != nil {
			t.Fatal(e)
		}
		task.AssetIDs = []string{asset.ID}
		s.store.Put("task", task.ID, task)
		made = append(made, task)
	}
	return s, made[0], made[1], &calls, &mu
}

func waitTask(t *testing.T, s *Service, runID, slot string, done func(Task) bool) Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tasks, _ := s.GetAgentTasks(runID)
		for _, task := range tasks {
			if task.PlanItemID == slot && done(task) {
				return task
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return Task{}
}

func TestRefineAgentImageKeepsVersionsAndExportsCurrent(t *testing.T) {
	s, first, second, calls, mu := refineFixture(t)
	bad := []RefineInput{
		{RequestID: "r0", TaskID: first.ID, AssetID: first.AssetIDs[0], Instruction: "  "},
		{RequestID: "r0", TaskID: first.ID, AssetID: second.AssetIDs[0], Instruction: "改横排"},
		{RequestID: "", TaskID: first.ID, AssetID: first.AssetIDs[0], Instruction: "改横排"},
		{RequestID: "r0", TaskID: first.ID, AssetID: first.AssetIDs[0], Instruction: strings.Repeat("长", 1001)},
	}
	for _, in := range bad {
		if _, e := s.RefineAgentImage(in); e == nil {
			t.Fatal("invalid refine accepted", in)
		}
	}
	in := RefineInput{RequestID: "refine-1", TaskID: first.ID, AssetID: first.AssetIDs[0], Instruction: "袖口文字改成横排"}
	if batch, e := s.RefineAgentImage(in); e != nil || batch != "batch-1" {
		t.Fatal(batch, e)
	}
	if _, e := s.RefineAgentImage(in); e != nil {
		t.Fatal("repeated click should return the same submission", e)
	}
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "refine-2", TaskID: first.ID, AssetID: first.AssetIDs[0], Instruction: "再改"}); e == nil {
		t.Fatal("second refine allowed while the slot is still queued")
	}
	all, _ := List[Task](s.store, "task")
	if len(all) != 3 {
		t.Fatal("expected exactly one new task", len(all))
	}
	s.Start(func() {})
	refined := waitTask(t, s, first.AgentRunID, "slot-1", func(task Task) bool { return task.Status == "succeeded" && task.RefinedFrom == first.ID })
	if len(refined.AssetIDs) != 1 || len(refined.PreviousAssetIDs) != 1 || refined.PreviousAssetIDs[0] != first.AssetIDs[0] {
		t.Fatalf("versions not kept: %+v", refined)
	}
	mu.Lock()
	call := (*calls)[0]
	mu.Unlock()
	if call.images != 1 || !strings.Contains(call.prompt, "本次修改（必须做到，改完后要能明显看出变化）：袖口文字改成横排") || !strings.Contains(call.prompt, "本次修改优先于原图现状") || strings.Contains(call.prompt, "文字方向与排列") || !strings.Contains(call.prompt, "原始提示词 slot-1") {
		t.Fatalf("unexpected edit request: %+v", call)
	}
	tasks, _ := s.GetAgentTasks(first.AgentRunID)
	if len(tasks) != 2 || tasks[1].ID != second.ID || len(tasks[1].PreviousAssetIDs) != 0 {
		t.Fatal("slots changed", tasks)
	}

	// A refinement of a refinement keeps a single wrapper around the original prompt.
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "refine-3", TaskID: refined.ID, AssetID: refined.AssetIDs[0], Instruction: "背景更亮"}); e != nil {
		t.Fatal(e)
	}
	again := waitTask(t, s, first.AgentRunID, "slot-1", func(task Task) bool { return task.Status == "succeeded" && task.RefinedFrom == refined.ID })
	if strings.Count(again.Prompt, refinePromptMarker) != 1 || strings.Contains(again.Prompt, "袖口文字改成横排") || len(again.PreviousAssetIDs) != 2 {
		t.Fatalf("nested refine prompt or lost versions: %q %v", again.Prompt, again.PreviousAssetIDs)
	}

	path := filepath.Join(t.TempDir(), "set.zip")
	if n, e := s.ExportBatch("batch-1", path); e != nil || n != 2 {
		t.Fatal("set should hold the current version of each slot", n, e)
	}
	zr, _ := zip.OpenReader(path)
	defer zr.Close()
	f, _ := zr.File[0].Open()
	data, _ := io.ReadAll(f)
	f.Close()
	var currentAsset Asset
	s.store.Get("asset", again.AssetIDs[0], &currentAsset)
	original, _ := readOwned(s.store.Root, currentAsset.Path)
	if string(data) != string(original) {
		t.Fatal("first ZIP entry is not the newest version of slot 1")
	}
}

func TestRefineRejectsNonAgentAndUnfinishedImages(t *testing.T) {
	s, first, _, _, _ := refineFixture(t)
	plain := first
	plain.ID, plain.AgentRunID, plain.PlanItemID = NewID(), "", ""
	s.store.Put("task", plain.ID, plain)
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "x", TaskID: plain.ID, AssetID: first.AssetIDs[0], Instruction: "改"}); e == nil {
		t.Fatal("workbench task refined")
	}
	first.Status = "unknown"
	s.store.Put("task", first.ID, first)
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "y", TaskID: first.ID, AssetID: first.AssetIDs[0], Instruction: "改"}); e == nil {
		t.Fatal("unfinished image refined")
	}
}

func TestWorkbenchRecentBatchesExcludeSmartGeneration(t *testing.T) {
	s, _, _, _, _ := refineFixture(t)
	plain := Task{ID: NewID(), BatchID: "plain", CreatedAt: Now(), UpdatedAt: Now(), Status: "succeeded", AssetIDs: []string{}, References: []string{}}
	s.store.Put("task", plain.ID, plain)
	batches, e := s.ListRecentBatches(6)
	if e != nil || len(batches) != 1 || batches[0].ID != "plain" {
		t.Fatal("smart generation batch shown on the workbench", batches, e)
	}
}

func TestBuiltinSkillUpgradeKeepsUserEdits(t *testing.T) {
	s := testService(t)
	var suite, embroidery SkillRecord
	s.store.Get("skill", "etsy-suite", &suite)
	s.store.Get("skill", "embroidery-rules", &embroidery)
	if !strings.Contains(suite.Content, "容易画坏的图") || !strings.Contains(embroidery.Content, "穿着者左袖") {
		t.Fatal("new installs miss current rules")
	}
	for _, b := range builtinSkills {
		if _, _, e := validateSkill(b.content()); e != nil {
			t.Fatal("built-in skill fails validation", b.id, e)
		}
	}
	// An unedited 0.3.1 copy (with the old description) is upgraded in place.
	suite.Content = builtinSkillContent("etsy-suite", etsySuiteDescriptionV1, etsySuiteBodyV2+designSpecRule)
	suite.Version = 3
	s.store.Put("skill", suite.ID, suite)
	embroidery.Content = strings.Replace(builtinSkillContent("embroidery-rules", embroideryDescriptionV1, embroideryBodyV1), "逐项检查", "按我的规则检查", 1)
	s.store.Put("skill", embroidery.ID, embroidery)
	if e := s.initializeAgents(); e != nil {
		t.Fatal(e)
	}
	s.store.Get("skill", "etsy-suite", &suite)
	s.store.Get("skill", "embroidery-rules", &embroidery)
	if suite.Version != 4 || !strings.Contains(suite.Content, "容易画坏的图") || suite.Description != builtinSkills[1].description {
		t.Fatal("unedited built-in skill not upgraded", suite.Version)
	}
	if !strings.Contains(embroidery.Content, "按我的规则检查") || strings.Contains(embroidery.Content, "阅读方向") {
		t.Fatal("user-edited skill was overwritten")
	}
}

func TestRefineStripsWrappersSavedBefore0611(t *testing.T) {
	old := Task{RefinedFrom: "earlier", Prompt: "以提供的图片为底图做局部微调。调整要求：袖口改横排" + refinePromptMarkerV1 + "原始提示词"}
	if baseTaskPrompt(old) != "原始提示词" {
		t.Fatal("old refine wrapper not stripped", baseTaskPrompt(old))
	}
	p := refinePrompt("原始提示词", "把狗掌移到名字左边", false)
	if strings.Index(p, "把狗掌移到名字左边") > strings.Index(p, "原始提示词") || baseTaskPrompt(Task{RefinedFrom: "x", Prompt: p}) != "原始提示词" {
		t.Fatal("change must come before the original prompt and strip cleanly", p)
	}
}
