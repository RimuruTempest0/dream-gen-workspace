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
	"sync/atomic"
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
	if !strings.Contains(suite.Content, "容易画坏的图") || !strings.Contains(embroidery.Content, "同一事实只定义一次") {
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
	p := refinePrompt("原始提示词", "把狗掌移到名字左边", 0)
	if strings.Index(p, "把狗掌移到名字左边") > strings.Index(p, "原始提示词") || baseTaskPrompt(Task{RefinedFrom: "x", Prompt: p}) != "原始提示词" {
		t.Fatal("change must come before the original prompt and strip cleanly", p)
	}
}

// A refinement whose original requirements contradict the change has them
// rewritten first, so the edit request no longer says the opposite.
func TestRefineRewritesConflictingRequirements(t *testing.T) {
	for _, scenario := range []string{"rewritten", "text lost"} {
		t.Run(scenario, func(t *testing.T) {
			s, first, _, calls, mu := refineFixture(t)
			first.Prompt = "主体：三只圣诞袜，只有中间一只带刺绣\n画面文字（逐字）：“LUNA”\n约束：左右两只袜子没有任何名字或图案"
			s.store.Put("task", first.ID, first)
			var asked atomic.Value
			llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				asked.Store(body.Messages[0].Content)
				reply := "主体：三只圣诞袜都带刺绣\n画面文字（逐字）：“LUNA”\n约束：三只袜子的袜口都带金毛头像和名字"
				if scenario == "text lost" {
					reply = "主体：三只圣诞袜都带刺绣，袜口都有金毛头像\n约束：三只袜子的袜口都带金毛头像和名字"
				}
				json.NewEncoder(w).Encode(map[string]any{"content": []map[string]string{{"type": "text", "text": reply}}})
			}))
			t.Cleanup(llm.Close)
			if _, e := s.SaveConnection(ConnectionInput{Kind: "llm", Name: "LLM", BaseURL: llm.URL, Model: "m", Key: "k"}); e != nil {
				t.Fatal(e)
			}
			if _, e := s.RefineAgentImage(RefineInput{RequestID: "r-" + scenario, TaskID: first.ID, AssetID: first.AssetIDs[0], Instruction: "三只袜子上都要有金毛+字母"}); e != nil {
				t.Fatal(e)
			}
			s.Start(func() {})
			done := waitTask(t, s, first.AgentRunID, "slot-1", func(task Task) bool {
				return task.RefinedFrom == first.ID && (task.Status == "succeeded" || task.Status == "failed")
			})
			if q, _ := asked.Load().(string); !strings.Contains(q, "用户的修改：三只袜子上都要有金毛+字母") || !strings.Contains(q, "只有中间一只带刺绣") {
				t.Fatalf("LLM not given the change and the original requirements: %q", q)
			}
			mu.Lock()
			sent := append([]editCall{}, (*calls)...)
			mu.Unlock()
			if scenario == "text lost" {
				if done.Status != "failed" || !strings.Contains(done.Error, "没有发送、不计费") || !strings.Contains(done.Error, "LUNA") || len(sent) != 0 {
					t.Fatalf("lost text was sent: %s %q %d", done.Status, done.Error, len(sent))
				}
				return
			}
			if done.Status != "succeeded" || len(sent) != 1 {
				t.Fatal(done.Status, done.Error, len(sent))
			}
			if p := sent[0].prompt; !strings.Contains(p, "三只圣诞袜都带刺绣") || strings.Contains(p, "只有中间一只") || strings.Contains(p, "左右两只袜子没有") || !strings.Contains(p, "本次修改（必须做到，改完后要能明显看出变化）：三只袜子上都要有金毛+字母") {
				t.Fatalf("edit still contradicts the change: %q", p)
			}
			callsLLM, _ := List[LLMCall](s.store, "llm_call")
			if len(callsLLM) != 1 || callsLLM[0].Stage != "优化前改写原制作要求" {
				t.Fatalf("%+v", callsLLM)
			}
		})
	}
}

func TestRefineRequirementsKeepInImageText(t *testing.T) {
	base := "主体：一只袜子\n画面文字（逐字）：“LUNA”\n约束：只有这一只带刺绣，其余没有图案"
	for _, c := range []struct {
		instruction, text, want string
	}{
		{"三只都要刺绣", "主体：三只袜子\n画面文字（逐字）：“LUNA”\n约束：三只都带刺绣，名字相同", ""},
		{"三只都要刺绣", "主体：三只袜子\n画面文字（逐字）：“LUNA”“MERRY”\n约束：三只都带刺绣，名字相同", "加入了"},
		{"三只都要刺绣", "主体：三只袜子都带刺绣\n约束：三只都带刺绣，名字相同，头像相同", "没有保留"},
		{"名字改成 MAX", "主体：一只袜子\n画面文字（逐字）：“MAX”\n约束：只有这一只带刺绣，其余没有图案", ""},
		{"去掉名字", "主体：一只袜子\n约束：只有这一只带头像刺绣，其余没有图案，没有文字", ""},
		{"三只都要刺绣", "三只", "短太多"},
	} {
		e := checkRefineRequirements(base, c.instruction, c.text)
		if (c.want == "") != (e == nil) || (e != nil && !strings.Contains(e.Error(), c.want)) {
			t.Errorf("%q → %v, want %q", c.text, e, c.want)
		}
	}
}

// A refinement later refused by the content check is reworded for the refusal,
// not merged with its requirements again.
func TestRefusedRefinementIsRewordedForTheRefusal(t *testing.T) {
	s := testService(t)
	c := saveConnection(t, s, "http://127.0.0.1:1/v1")
	s.store.Get("connection", c.ID, &c) // with its stored key reference
	refined := Task{ID: NewID(), BatchID: "b", CreatedAt: Now(), UpdatedAt: Now(), Status: "failed", Prompt: "p", Model: "m", Connection: c, References: []string{}, AssetIDs: []string{}, RefinedFrom: "x", RewordFor: rewordRefine, PolicyStep: 1}
	s.mu.Lock()
	next, e := s.queueAttemptLocked(refined, c, "")
	s.mu.Unlock()
	if e != nil || next.RewordFor != "" || next.Rewording {
		t.Fatalf("new attempt kept the refine rewrite: %+v %v", next, e)
	}
}
