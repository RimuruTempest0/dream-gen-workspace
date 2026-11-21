package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 逐张核验 (#20): per-image verdicts on a 套图, next to the suite's verdict.

type itemSuite struct {
	run AgentRun
	// tasks by name: worn (first version), wornRefined (its refinement),
	// flat (failed), box (结果未知), detail (paused).
	tasks map[string]Task
}

// putItemSuite saves a submitted suite of four slots: a worn shot refined once,
// a failed flat lay, a gift box with an unknown result and a paused close-up.
func putItemSuite(t *testing.T, s *Service) itemSuite {
	t.Helper()
	run := AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: "submitted", BatchID: NewID(), SubmittedRevision: 1,
		Input: AgentInput{Instruction: "4 张刺绣卫衣套图"},
		Plans: []SuitePlan{{Revision: 1, Items: []SuiteItem{
			{ID: "p-worn", Purpose: "上身主图"}, {ID: "p-flat", Purpose: "整件平铺"}, {ID: "p-box", Purpose: "礼盒"}, {ID: "p-detail", Purpose: "袖口近景"},
		}}}}
	if e := s.store.Put("agent_run", run.ID, run); e != nil {
		t.Fatal(e)
	}
	base := time.Now().UTC()
	at := func(i int) string { return base.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano) }
	out := itemSuite{run: run, tasks: map[string]Task{}}
	put := func(name, slot, status string, i int, edit func(*Task)) {
		task := Task{ID: "task-" + name, BatchID: run.BatchID, AgentRunID: run.ID, PlanItemID: slot, CreatedAt: at(i), UpdatedAt: at(i), Status: status, Prompt: "提示词 " + name, AssetIDs: []string{}}
		if status == "succeeded" {
			task.AssetIDs = []string{"asset-" + name}
		}
		if edit != nil {
			edit(&task)
		}
		if e := s.store.Put("task", task.ID, task); e != nil {
			t.Fatal(e)
		}
		out.tasks[name] = task
	}
	put("worn", "p-worn", "succeeded", 0, nil)
	put("flat", "p-flat", "failed", 1, func(t *Task) { t.Error = "线路额度已用完" })
	put("box", "p-box", "unknown", 2, nil)
	put("detail", "p-detail", "blocked", 3, nil)
	put("wornRefined", "p-worn", "succeeded", 4, func(t *Task) { t.RefinedFrom, t.RefineInstruction = "task-worn", "亮一点" })
	return out
}

func TestFeedbackImagesListEveryCardAndEveryVersionOfTheSuite(t *testing.T) {
	s := testService(t)
	suite := putItemSuite(t, s)
	images, e := s.GetFeedbackImages(suite.run.ID)
	if e != nil {
		t.Fatal(e)
	}
	got := []string{}
	for _, v := range images {
		got = append(got, fmt.Sprintf("%d %s %s v%d/%d current=%v %s %q", v.Slot, v.Purpose, v.TaskID, v.Version, v.Versions, v.Current, v.Status, v.RefineInstruction))
	}
	// Slot order of the plan; the current version of a slot first, then earlier ones.
	want := []string{
		`1 上身主图 task-wornRefined v2/2 current=true succeeded "亮一点"`,
		`1 上身主图 task-worn v1/2 current=false succeeded ""`,
		`2 整件平铺 task-flat v1/1 current=true failed ""`,
		`3 礼盒 task-box v1/1 current=true unknown ""`,
		`4 袖口近景 task-detail v1/1 current=true blocked ""`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("images:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if images[0].PlanItemID != "p-worn" || len(images[0].AssetIDs) != 1 || images[0].AssetIDs[0] != "asset-wornRefined" || images[2].Error != "线路额度已用完" {
		t.Fatalf("image facts: %+v", images)
	}
	if _, e = s.GetFeedbackImages("missing"); e == nil {
		t.Fatal("listed a suite that does not exist")
	}
}

func TestPerImageVerdictsAreKeptOnTheirSlotAndVersion(t *testing.T) {
	s := testService(t)
	suite := putItemSuite(t, s)
	items := []FeedbackItem{
		{TaskID: "task-worn", Verdict: "ok", Note: "第一版其实更好"},
		{TaskID: "task-wornRefined", Verdict: "problem", Problems: []string{"edit-missed"}, Note: "我要的是亮一点，实际更暗了"},
		{TaskID: "task-flat", Verdict: "problem", Problems: []string{"no-image"}},
		{TaskID: "task-box", Verdict: "problem", Problems: []string{"no-image", "other"}},
		{TaskID: "task-detail", Verdict: "problem", Note: "暂停后没有继续"},
	}
	saved, e := s.SaveSuiteFeedback(SuiteFeedback{RunID: suite.run.ID, Verdict: "partly-usable", Problems: []string{"edit-missed"}, Items: items})
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.GetSuiteFeedback(suite.run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(saved.Items) != len(items) || len(got.Items) != len(items) {
		t.Fatalf("saved %+v, read back %+v", saved.Items, got.Items)
	}
	slots := map[string]string{"task-worn": "p-worn", "task-wornRefined": "p-worn", "task-flat": "p-flat", "task-box": "p-box", "task-detail": "p-detail"}
	for i, want := range items {
		v := got.Items[i]
		if v.TaskID != want.TaskID || v.PlanItemID != slots[want.TaskID] || v.Verdict != want.Verdict || strings.Join(v.Problems, ",") != strings.Join(want.Problems, ",") || v.Note != want.Note {
			t.Errorf("item %d: got %+v, want %+v on %s", i, v, want, slots[want.TaskID])
		}
	}
	// Per-image verdicts are optional: the suite verdict alone still saves, and
	// clears the earlier per-image ones.
	if got, e = s.SaveSuiteFeedback(SuiteFeedback{RunID: suite.run.ID, Verdict: "unusable", Note: "整套都不行"}); e != nil || len(got.Items) != 0 {
		t.Fatalf("suite verdict alone: %+v %v", got, e)
	}
}

func TestPerImageVerdictsMustNameAnImageOfTheSuite(t *testing.T) {
	s := testService(t)
	suite := putItemSuite(t, s)
	foreign := Task{ID: "task-foreign", AgentRunID: NewID(), PlanItemID: "p-worn", CreatedAt: Now(), Status: "succeeded", AssetIDs: []string{}}
	s.store.Put("task", foreign.ID, foreign)
	for name, item := range map[string]FeedbackItem{
		"no verdict":           {TaskID: "task-worn", Problems: []string{"other"}},
		"unknown verdict":      {TaskID: "task-worn", Verdict: "great"},
		"unknown problem":      {TaskID: "task-worn", Verdict: "problem", Problems: []string{"ugly"}},
		"another suite's":      {TaskID: "task-foreign", Verdict: "ok"},
		"no such image":        {TaskID: "task-missing", Verdict: "ok"},
		"nothing named":        {Verdict: "ok"},
		"slot not in a plan":   {PlanItemID: "p-missing", Verdict: "ok"},
		"note too long":        {TaskID: "task-worn", Verdict: "problem", Note: strings.Repeat("长", 2001)},
		"slot of another task": {TaskID: "task-worn", PlanItemID: "p-flat", Verdict: "ok"},
	} {
		if _, e := s.SaveSuiteFeedback(SuiteFeedback{RunID: suite.run.ID, Verdict: "usable", Items: []FeedbackItem{item}}); e == nil {
			t.Errorf("%s: saved", name)
		}
	}
	if got, _ := s.GetSuiteFeedback(suite.run.ID); got.Verdict != "" {
		t.Fatalf("a refused save was kept: %+v", got)
	}
	// A slot can be marked without a version; an "ok" image keeps no problem types.
	got, e := s.SaveSuiteFeedback(SuiteFeedback{RunID: suite.run.ID, Verdict: "usable", Items: []FeedbackItem{
		{PlanItemID: "p-box", Verdict: "problem", Problems: []string{"other"}},
		{TaskID: "task-flat", Verdict: "ok", Problems: []string{"other"}, Note: "  能用  "},
	}})
	if e != nil || got.Items[0].PlanItemID != "p-box" || got.Items[1].PlanItemID != "p-flat" || len(got.Items[1].Problems) != 0 || got.Items[1].Note != "能用" {
		t.Fatalf("%+v %v", got, e)
	}
}

func TestFeedbackPackageCarriesThePerImageVerdicts(t *testing.T) {
	s := testService(t)
	suite := putItemSuite(t, s)
	note := "我要的是亮一点，实际更暗了"
	if _, e := s.SaveSuiteFeedback(SuiteFeedback{RunID: suite.run.ID, Verdict: "partly-usable", Note: "第 1 张改坏了", Items: []FeedbackItem{
		{TaskID: "task-wornRefined", Verdict: "problem", Problems: []string{"edit-missed", "product-changed"}, Note: note},
		{TaskID: "task-worn", Verdict: "ok"},
		{TaskID: "task-box", Verdict: "problem", Problems: []string{"no-image"}},
	}}); e != nil {
		t.Fatal(e)
	}
	_, files := feedbackZip(t, s, suite.run.ID)
	md := string(files["反馈.md"])
	section := strings.Index(md, "## 逐张结论")
	if section < 0 || section > strings.Index(md, "## 用户原要求") {
		t.Fatalf("反馈.md has no per-image section before the request:\n%s", md)
	}
	// Each verdict names its slot, its version and the refinement asked for, in Chinese.
	for _, want := range []string{
		"第 1 张（上身主图）第 2 版（当前，共 2 版）",
		"修改要求：亮一点",
		"有问题",
		"商品本身被改了；改图没改到",
		note,
		"第 1 张（上身主图）第 1 版（共 2 版）",
		"合格",
		"第 3 张（礼盒）第 1 版（当前，共 1 版）",
		"结果未知",
		"没出图或报错",
		"task-wornRefined",
	} {
		if !strings.Contains(md[section:], want) {
			t.Errorf("反馈.md per-image section lacks %q:\n%s", want, md[section:])
		}
	}
	var pkg struct {
		Feedback SuiteFeedback `json:"feedback"`
		Tasks    []struct {
			ID         string `json:"id"`
			PlanItemID string `json:"planItemID"`
		} `json:"tasks"`
	}
	if e := json.Unmarshal(files["run.json"], &pkg); e != nil {
		t.Fatal(e)
	}
	if len(pkg.Feedback.Items) != 3 || pkg.Feedback.Items[0].TaskID != "task-wornRefined" || pkg.Feedback.Items[0].PlanItemID != "p-worn" || pkg.Feedback.Items[0].Note != note || strings.Join(pkg.Feedback.Items[0].Problems, ",") != "product-changed,edit-missed" {
		t.Fatalf("run.json feedback items: %+v", pkg.Feedback.Items)
	}
	// Every verdict's version is one of the package's tasks, on the same slot.
	byID := map[string]string{}
	for _, v := range pkg.Tasks {
		byID[v.ID] = v.PlanItemID
	}
	for _, v := range pkg.Feedback.Items {
		if slot, ok := byID[v.TaskID]; !ok || slot != v.PlanItemID {
			t.Errorf("item %+v has no matching task in run.json", v)
		}
	}
}
