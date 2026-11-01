package core

import (
	"fmt"
	"testing"
	"time"
)

func TestCurrentSmartProgressFollowsOnlyTheNewestRun(t *testing.T) {
	s := testService(t)
	put := func(r AgentRun) { s.store.Put("agent_run", r.ID, r) }
	created := func(minutes int) string {
		return time.Date(2026, 10, 19, 8, minutes, 0, 0, time.UTC).Format(time.RFC3339Nano)
	}
	// An old abandoned request waiting for input must not pull the user back.
	put(AgentRun{ID: "old", CreatedAt: created(0), Status: "needs_input"})
	newest := AgentRun{ID: "new", CreatedAt: created(10), Status: "analyzing"}
	put(newest)
	if p, _ := s.CurrentSmartProgress(); p.RunID != "new" || p.State != "analyzing" {
		t.Fatal("analyzing newest run not reported", p)
	}

	newest.Status, newest.BatchID = "submitted", "b"
	put(newest)
	statuses := []string{"succeeded", "succeeded", "submitting", "queued"}
	for i, status := range statuses {
		task := Task{ID: NewID(), BatchID: "b", AgentRunID: "new", PlanItemID: fmt.Sprintf("slot-%d", i), CreatedAt: created(11), Status: status, AssetIDs: []string{}, References: []string{}}
		s.store.Put("task", task.ID, task)
	}
	p, _ := s.CurrentSmartProgress()
	if p.RunID != "new" || p.State != "generating" || p.Done != 2 || p.Total != 4 {
		t.Fatal("generation progress wrong", p)
	}

	tasks, _ := List[Task](s.store, "task")
	for _, task := range tasks {
		switch task.Status {
		case "submitting":
			task.Status = "succeeded"
		case "queued":
			task.Status = "blocked"
		}
		s.store.Put("task", task.ID, task)
	}
	if p, _ := s.CurrentSmartProgress(); p.State != "paused" || p.Done != 3 {
		t.Fatal("paused images not reported", p)
	}

	tasks, _ = List[Task](s.store, "task")
	for _, task := range tasks {
		task.Status = "succeeded"
		s.store.Put("task", task.ID, task)
	}
	if p, _ := s.CurrentSmartProgress(); p.RunID != "" {
		t.Fatal("finished run should open a new request", p)
	}
}

func TestWaitingForAnswersIsLeftOutOfTheTimer(t *testing.T) {
	stopped := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339Nano)
	for _, status := range []string{"needs_input", "review", "interrupted", "failed"} {
		r := AgentRun{Status: status, UpdatedAt: stopped, WaitedMS: 1000}
		resumeTimer(&r)
		if r.WaitedMS < 1000+299_000 || r.WaitedMS > 1000+310_000 {
			t.Fatal(status, "waited", r.WaitedMS)
		}
	}
	// A run that was working, or ready to submit, did not wait for the user.
	for _, status := range []string{"analyzing", "ready", "submitted"} {
		r := AgentRun{Status: status, UpdatedAt: stopped}
		resumeTimer(&r)
		if r.WaitedMS != 0 {
			t.Fatal(status, "counted as waiting", r.WaitedMS)
		}
	}
}
