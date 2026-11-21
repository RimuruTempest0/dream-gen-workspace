package core

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Two attempts of a slot within one second, stored as earlier versions wrote
// them: trailing zeros dropped, so the newer ".55Z" sorts before the older
// ".5Z" as text.
func sameSecond() (older, newer string) {
	base := time.Now().UTC().Truncate(time.Second)
	return base.Add(500 * time.Millisecond).Format(time.RFC3339Nano), base.Add(550 * time.Millisecond).Format(time.RFC3339Nano)
}

// The current version of a slot is its newest attempt by time, not by stored
// text: the suite page, the set's ZIP and 反馈.md all show the newer one.
func TestCurrentVersionIsTheNewestAttemptWithinTheSameSecond(t *testing.T) {
	s := testService(t)
	older, newer := sameSecond()
	if !strings.HasSuffix(older, ".5Z") || !strings.HasSuffix(newer, ".55Z") {
		t.Fatalf("fixture times %s %s", older, newer)
	}
	run := putFeedbackRun(t, s, "submitted")
	run.BatchID = "batch-" + run.ID
	run.Plans = []SuitePlan{{Revision: 1, Items: []SuiteItem{{ID: "slot-1", Purpose: "主图"}}}}
	run.SubmittedRevision = 1
	s.store.Put("agent_run", run.ID, run)
	first := Task{ID: "task-old", BatchID: run.BatchID, AgentRunID: run.ID, PlanItemID: "slot-1", CreatedAt: older, UpdatedAt: older, Status: "failed", References: []string{}}
	second := Task{ID: "task-new", BatchID: run.BatchID, AgentRunID: run.ID, PlanItemID: "slot-1", CreatedAt: newer, UpdatedAt: newer, Status: "succeeded", References: []string{}}
	oldImage, newImage := photoPNG(8, 8, 1), photoPNG(8, 8, 2)
	s.mu.Lock()
	a := putAsset(t, s, oldImage, "generated", first.ID)
	b := putAsset(t, s, newImage, "generated", second.ID)
	s.mu.Unlock()
	first.AssetIDs, second.AssetIDs = []string{a.ID}, []string{b.ID}
	// Stored as the queue stores attempts: ordered by their creation time.
	if _, e := s.store.Submit("attempts", run.BatchID, []Task{first, second}); e != nil {
		t.Fatal(e)
	}

	tasks, e := s.GetAgentTasks(run.ID)
	if e != nil || len(tasks) != 1 || tasks[0].ID != second.ID || !slices.Equal(tasks[0].PreviousAssetIDs, []string{a.ID}) {
		t.Fatalf("current attempt: %+v %v", tasks, e)
	}

	path := filepath.Join(t.TempDir(), "set.zip")
	if n, e := s.ExportBatch(run.BatchID, path); e != nil || n != 1 {
		t.Fatal("export", n, e)
	}
	files := readZip(t, path)
	if len(files) != 1 || !bytes.Equal(files["001.png"], newImage) {
		t.Fatalf("the set's ZIP does not hold the current version (%d files)", len(files))
	}

	_, pkg := feedbackZip(t, s, run.ID)
	if row := mdRow(string(pkg["反馈.md"]), "—", "（未记录）", "第 1 张", "主图"); !strings.HasSuffix(row, "| 已完成 |") {
		t.Fatalf("反馈.md shows the older attempt as the final state: %q", row)
	}
}

// An image drawn from a sample gets the sample's newest attempt, even when an
// older failed attempt was made within the same second.
func TestImageGetsTheSamplesNewestAttemptWithinTheSameSecond(t *testing.T) {
	server, _, _ := relay(t, 500, `{"error":{"message":"upstream"}}`)
	s := testService(t)
	c := storedConnection(t, s, server.URL)
	older, newer := sameSecond()
	runID := NewID()
	failed := suiteTask(c, runID, "sample", "failed")
	failed.CreatedAt, failed.Sample, failed.SampleArea = older, true, "胸前"
	redrawn := suiteTask(c, runID, "sample", "succeeded")
	redrawn.CreatedAt, redrawn.Sample, redrawn.SampleArea, redrawn.SampleCheck = newer, true, "胸前", "passed"
	s.mu.Lock()
	sampleImage := putAsset(t, s, tinyPNG(), "generated", redrawn.ID)
	s.mu.Unlock()
	redrawn.AssetIDs = []string{sampleImage.ID}
	image := suiteTask(c, runID, "main", "queued")
	image.SampleItems, image.SampleItem, image.SamplePending = []string{"sample"}, "sample", true
	if _, e := s.store.Submit("suite", "batch-"+runID, []Task{failed, redrawn, image}); e != nil {
		t.Fatal(e)
	}
	// The scheduler resolves the samples when it picks the image.
	if !s.claimTask(false) {
		t.Fatal("the image was not picked")
	}
	s.workers.Wait()
	got, e := s.GetTask(image.ID)
	if e != nil || !slices.Equal(got.SampleAssetIDs, []string{sampleImage.ID}) || got.SamplePending {
		t.Fatalf("the image did not get the sample's newest attempt: %v pending=%v %v", got.SampleAssetIDs, got.SamplePending, e)
	}
}
