package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// mdRow finds the 反馈.md table row that starts with the given cells.
func mdRow(md string, cells ...string) string {
	prefix := "| " + strings.Join(cells, " | ") + " |"
	for _, l := range strings.Split(md, "\r\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}

// The Agent lists the customer's requests when it saves the plan and says which
// request and 展示方式 each image carries out; 反馈.md sets them side by side, so
// a requested image the plan left out shows.
func TestFeedbackPackageSetsTheRequestsAgainstThePlan(t *testing.T) {
	s := testService(t)
	r := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "analyzing", Input: AgentInput{Instruction: "1 上身主图；2 整件平铺；3 礼盒", TargetCount: 2, References: []string{}}}
	s.store.Put("agent_run", r.ID, r)
	b := &agentBridge{service: s, runID: r.ID, ctx: context.Background()}
	plan := simplePlan(2, []string{})
	plan.Items[0].Purpose, plan.Items[1].Purpose = "主图", "补充细节"
	first, _ := json.Marshal(map[string]any{"locked": plan.Locked, "editable": plan.Editable, "append": true,
		"requests": []string{"上身主图", "整件平铺", "礼盒"},
		"items":    []map[string]any{{"purpose": "主图", "scene": "客厅", "prompt": "p1", "references": []string{}, "request": 1, "presentation": "上身"}}})
	if _, e := b.callTool("save_suite_plan", first); e != nil {
		t.Fatal(e)
	}
	// The second image serves no request (a supplementary image).
	last, _ := json.Marshal(map[string]any{"items": []map[string]any{{"purpose": "补充细节", "scene": "桌面", "prompt": "p2", "references": []string{}, "presentation": "细节近景"}}})
	if _, e := b.callTool("save_suite_plan", last); e != nil {
		t.Fatal(e)
	}
	saved, _ := s.GetAgentRun(r.ID)
	if len(saved.Plans) != 1 {
		t.Fatalf("plan not saved: %+v", saved.Plans)
	}
	item := saved.Plans[0].Items[0]
	s.store.Put("task", "t1", Task{ID: "t1", BatchID: "b", AgentRunID: r.ID, PlanItemID: item.ID, CreatedAt: Now(), UpdatedAt: Now(), Status: "succeeded", AssetIDs: []string{}})
	_, files := feedbackZip(t, s, r.ID)
	md := string(files["反馈.md"])
	if !strings.Contains(md, "| 序号 | 顾客原要求 | 图位 | 方案用途 | 展示方式 | 场景 | 样板 | 最终状态 |") {
		t.Fatalf("no comparison table:\n%s", md)
	}
	if mdRow(md, "1", "上身主图", "第 1 张", "主图", "上身", "客厅") == "" || !strings.Contains(mdRow(md, "1", "上身主图"), "已完成") {
		t.Fatalf("request 1 not set against its image:\n%s", md)
	}
	// Requested, but no image of the plan carries it out.
	for _, missing := range [][]string{{"2", "整件平铺"}, {"3", "礼盒"}} {
		if row := mdRow(md, missing...); !strings.Contains(row, "方案里没有") {
			t.Fatalf("request %s left out of the plan does not show: %q\n%s", missing[0], row, md)
		}
	}
	if mdRow(md, "—", "（未对应原要求）", "第 2 张", "补充细节", "细节近景") == "" {
		t.Fatalf("supplementary image not listed:\n%s", md)
	}
}

// exportedTasks lists the run's tasks as run.json writes them.
func exportedTasks(t *testing.T, s *Service, runID string) []map[string]any {
	t.Helper()
	_, files := feedbackZip(t, s, runID)
	var pkg struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if e := json.Unmarshal(files["run.json"], &pkg); e != nil {
		t.Fatal(e)
	}
	return pkg.Tasks
}

// Each new attempt records who started it: the automatic resend, the customer
// regenerating one image, the one-click completion. A first attempt and a task
// stored before this was recorded carry no trigger; none is made up for them.
func TestRegenerationsRecordWhoTriggeredThem(t *testing.T) {
	server, ok, _ := relay(t, 524, `{"error":{"message":"error code: 524"}}`)
	s := testService(t)
	c := storedConnection(t, s, server.URL)
	run := putFeedbackRun(t, s, "submitted")
	first := suiteTask(c, run.ID, "slot-1", "queued")
	if _, e := s.store.Submit("suite", "batch-"+run.ID, []Task{first}); e != nil {
		t.Fatal(e)
	}
	for s.stepNext() { // 结果未知, resent automatically once, 结果未知 again
	}
	failed := suiteTask(c, run.ID, "slot-2", "failed")
	old := suiteTask(c, run.ID, "slot-3", "succeeded") // an old retry: no trigger stored
	old.CreatedAt = Now()
	s.store.Put("task", failed.ID, failed)
	s.store.Put("task", old.ID, old)
	if _, e := s.Retry(failed.ID); e != nil {
		t.Fatal(e)
	}
	ok.Store(true)
	if n, e := s.RetryFailedAgentImages(run.ID); e != nil || n != 1 {
		t.Fatal("one-click completion", n, e)
	}
	all, _ := List[Task](s.store, "task")
	got := map[string][]string{}
	for i := len(all) - 1; i >= 0; i-- { // oldest first
		got[all[i].PlanItemID] = append(got[all[i].PlanItemID], all[i].Trigger)
	}
	if strings.Join(got["slot-1"], ",") != ",auto-resend,retry-all" || strings.Join(got["slot-2"], ",") != ",retry" || strings.Join(got["slot-3"], ",") != "" {
		t.Fatalf("triggers %v", got)
	}
	// run.json carries the trigger; the old task has none.
	byID := map[string]string{}
	for _, task := range exportedTasks(t, s, run.ID) {
		trigger, has := task["trigger"]
		if task["id"] == old.ID || task["id"] == first.ID || task["id"] == failed.ID {
			if has {
				t.Fatalf("a trigger was made up for %v: %v", task["id"], trigger)
			}
			continue
		}
		byID[task["planItemID"].(string)] += fmt.Sprint(trigger) + ","
	}
	if byID["slot-1"] != "auto-resend,retry-all," || byID["slot-2"] != "retry," {
		t.Fatalf("run.json triggers %v", byID)
	}
}

// The line's quota runs out, the rest of the set pauses unsent, and the
// customer's one-click completion resumes it: the timeline in 反馈.md still says
// when and why each image paused and that the one-click completion resumed it.
func TestFeedbackTimelineKeepsPausesAfterTheyAreResumed(t *testing.T) {
	server, ok, _ := relay(t, 402, `{"error":{"message":"该令牌额度已用尽 !token.UnlimitedQuota && token.RemainQuota = 0"}}`)
	s := testService(t)
	c := storedConnection(t, s, server.URL)
	run := putFeedbackRun(t, s, "submitted")
	run.Plans = []SuitePlan{{Revision: 1, Items: []SuiteItem{{ID: "slot-1", Purpose: "主图"}, {ID: "slot-2", Purpose: "整件平铺"}, {ID: "slot-3", Purpose: "礼盒"}}}}
	run.SubmittedRevision = 1
	s.store.Put("agent_run", run.ID, run)
	tasks := []Task{}
	for i := 1; i <= 3; i++ {
		tasks = append(tasks, suiteTask(c, run.ID, fmt.Sprintf("slot-%d", i), "queued"))
	}
	if _, e := s.store.Submit("suite", "batch-"+run.ID, tasks); e != nil {
		t.Fatal(e)
	}
	for s.stepNext() { // the first is refused, the other two pause
	}
	names := map[string]string{"slot-1": "第 1 张（主图）", "slot-2": "第 2 张（整件平铺）", "slot-3": "第 3 张（礼盒）"}
	paused := []string{}
	for _, task := range tasksWhere(s, func(t Task) bool { return t.Status == "blocked" }) {
		paused = append(paused, names[task.PlanItemID])
	}
	if len(paused) != 2 {
		t.Fatal("the rest of the set did not pause")
	}
	ok.Store(true)
	if n, e := s.RetryFailedAgentImages(run.ID); e != nil || n != 3 {
		t.Fatal("one-click completion", n, e)
	}
	for s.stepNext() {
	}
	_, files := feedbackZip(t, s, run.ID)
	md := string(files["反馈.md"])
	timeline := md[strings.Index(md, "## 时间线"):]
	for _, slot := range paused {
		p := strings.Index(timeline, slot+"：已暂停（线路额度已用完")
		r := strings.Index(timeline, slot+"：继续生成（顾客一键补齐")
		d := strings.Index(timeline, slot+"：已完成")
		if p < 0 || r < p || d < r {
			t.Fatalf("%s: pause, resume and result not in order:\n%s", slot, timeline)
		}
	}
	var pkg struct {
		Tasks []struct {
			PlanItemID string `json:"planItemID"`
			Pauses     []struct {
				At        string `json:"at"`
				Reason    string `json:"reason"`
				ResumedAt string `json:"resumedAt"`
				ResumedBy string `json:"resumedBy"`
			} `json:"pauses"`
		} `json:"tasks"`
	}
	if e := json.Unmarshal(files["run.json"], &pkg); e != nil {
		t.Fatal(e)
	}
	resumed := 0
	for _, task := range pkg.Tasks {
		for _, p := range task.Pauses {
			if p.At == "" || !strings.Contains(p.Reason, "线路额度已用完") || p.ResumedAt == "" || p.ResumedBy != "retry-all" {
				t.Fatalf("pause record of %s: %+v", task.PlanItemID, p)
			}
			resumed++
		}
	}
	if resumed != 2 {
		t.Fatalf("run.json keeps %d pauses, want 2", resumed)
	}
}

// A change to an image is a regeneration the customer started by asking for it.
func TestRefinementRecordsItsTrigger(t *testing.T) {
	s, first, _, _, _ := refineFixture(t)
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "r1", TaskID: first.ID, AssetID: first.AssetIDs[0], Instruction: "亮一点"}); e != nil {
		t.Fatal(e)
	}
	tasks, _ := s.GetAgentTasks(first.AgentRunID)
	for _, task := range tasks {
		if task.RefinedFrom == first.ID {
			if task.Trigger != "refine" {
				t.Fatalf("refinement trigger %q", task.Trigger)
			}
			return
		}
	}
	t.Fatal("no refinement queued")
}

// A plan saved before the Agent recorded requests and 展示方式 shows them as not
// recorded, never guessed.
func TestFeedbackPackageShowsAnOldPlansRequestsAsNotRecorded(t *testing.T) {
	s := testService(t)
	run := putFeedbackRun(t, s, "ready")
	plan := simplePlan(2, []string{})
	plan.Items[0].ID, plan.Items[1].ID = "a", "b"
	run.Plans = []SuitePlan{plan}
	s.store.Put("agent_run", run.ID, run)
	_, files := feedbackZip(t, s, run.ID)
	md := string(files["反馈.md"])
	if mdRow(md, "—", "（未记录）", "第 1 张", "图1", "（未记录）", "客厅", "", "未提交") == "" || mdRow(md, "—", "（未记录）", "第 2 张", "图2") == "" {
		t.Fatalf("old plan rows:\n%s", md)
	}
	if strings.Contains(md, "方案里没有") {
		t.Fatalf("an old plan was said to leave out requests it never recorded:\n%s", md)
	}
}

// A suite whose sample was redrawn, whose image came back 结果未知 and was resent
// automatically, then regenerated by the customer, and whose last image paused:
// the timeline in 反馈.md writes these in the order they happened.
func TestFeedbackTimelineTellsHowTheSuiteWentInOrder(t *testing.T) {
	s := testService(t)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	// Times of differing precision, as Now() writes them.
	at := func(sec int) string {
		return base.Add(time.Duration(sec)*time.Second + time.Duration(sec%3)*100*time.Millisecond).Format(time.RFC3339Nano)
	}
	run := putFeedbackRun(t, s, "submitted")
	run.CreatedAt = at(0)
	run.Plans = []SuitePlan{{Revision: 1, Items: []SuiteItem{
		{ID: "s", Purpose: "胸前样板", Sample: true, SampleArea: "胸前"}, {ID: "a", Purpose: "主图"}, {ID: "b", Purpose: "整件平铺"}, {ID: "c", Purpose: "礼盒"}}}}
	run.SubmittedRevision = 1
	s.store.Put("agent_run", run.ID, run)
	task := func(id, slot string, created, updated int, status string, edit func(*Task)) {
		v := Task{ID: id, BatchID: "b", AgentRunID: run.ID, PlanItemID: slot, CreatedAt: at(created), UpdatedAt: at(updated), Status: status, AssetIDs: []string{}}
		if edit != nil {
			edit(&v)
		}
		s.store.Put("task", v.ID, v)
	}
	task("s1", "s", 1, 2, "succeeded", func(v *Task) {
		v.Sample, v.SampleArea, v.SampleCheck, v.SampleProblems = true, "胸前", "redrawn", "字母 D 少了一笔"
	})
	task("s2", "s", 3, 4, "succeeded", func(v *Task) {
		v.Sample, v.SampleArea, v.SampleCheck, v.SampleRedraws, v.Trigger = true, "胸前", "passed", 1, triggerSampleRedraw
	})
	task("a1", "a", 5, 6, "unknown", func(v *Task) { v.AutoRetriedAs = "a2" })
	task("a2", "a", 7, 8, "unknown", func(v *Task) { v.AutoRetryOf, v.Trigger = "a1", triggerAutoResend })
	task("b1", "b", 9, 11, "blocked", func(v *Task) { v.Error = "线路额度已用完，尚未提交、未计费" })
	task("a3", "a", 10, 12, "succeeded", func(v *Task) { v.Trigger = triggerRetry })
	// An old second attempt: who started it was not recorded.
	task("c1", "c", 13, 14, "failed", func(v *Task) { v.Error = "upstream 500" })
	task("c2", "c", 15, 16, "succeeded", nil)
	_, files := feedbackZip(t, s, run.ID)
	md := string(files["反馈.md"])
	timeline := md[strings.Index(md, "## 时间线"):]
	want := []string{
		at(0) + " 开始",
		at(1) + " 第 1 张（胸前样板）：提交生成",
		at(2) + " 第 1 张（胸前样板）：样板核对不通过，已自动重画（字母 D 少了一笔）",
		at(3) + " 第 1 张（胸前样板）：样板自动重画（第 1 次）",
		at(4) + " 第 1 张（胸前样板）：样板核对通过",
		at(5) + " 第 2 张（主图）：提交生成",
		at(6) + " 第 2 张（主图）：结果未知",
		at(7) + " 第 2 张（主图）：自动重发",
		at(8) + " 第 2 张（主图）：结果未知",
		at(9) + " 第 3 张（整件平铺）：提交生成",
		at(10) + " 第 2 张（主图）：顾客单张重新生成",
		at(11) + " 第 3 张（整件平铺）：已暂停（线路额度已用完，尚未提交、未计费）",
		at(12) + " 第 2 张（主图）：已完成",
		at(14) + " 第 4 张（礼盒）：失败（upstream 500）",
		at(15) + " 第 4 张（礼盒）：重新生成（未记录触发方式）",
	}
	last := -1
	for _, w := range want {
		i := strings.Index(timeline, "- "+w+"\r\n")
		if i < 0 || i < last {
			t.Fatalf("timeline lacks %q in order:\n%s", w, timeline)
		}
		last = i
	}
}
