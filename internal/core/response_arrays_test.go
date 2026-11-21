package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// nullFields lists the JSON fields of v that are null.
func nullFields(t *testing.T, v any, fields ...string) []string {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	out := []string{}
	for _, f := range fields {
		if strings.Contains(string(b), `"`+f+`":null`) {
			out = append(out, f)
		}
	}
	return out
}

// A run or task stored without some lists (older versions, or written before
// they were filled) comes back with empty lists, never null: the suite page
// reads their length.
func TestStoredRunsAndTasksWithoutListsComeBackWithEmptyLists(t *testing.T) {
	s := testService(t)
	run := AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: "submitted", Plans: []SuitePlan{{Revision: 1, Items: []SuiteItem{{ID: "slot-1"}}}}, SubmittedRevision: 1}
	if e := s.store.Put("agent_run", run.ID, run); e != nil {
		t.Fatal(e)
	}
	task := Task{ID: NewID(), BatchID: "b", AgentRunID: run.ID, PlanItemID: "slot-1", CreatedAt: Now(), UpdatedAt: Now(), Status: "succeeded"}
	if e := s.store.Put("task", task.ID, task); e != nil {
		t.Fatal(e)
	}
	runFields := []string{"references", "skillIDs", "instructions", "skills", "questions", "plans", "events", "referenceIDs", "locked", "editable", "items"}
	got, e := s.GetAgentRun(run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if nulls := nullFields(t, got, runFields...); len(nulls) > 0 {
		t.Fatalf("GetAgentRun sends null for %v", nulls)
	}
	page, e := s.ListAgentRuns(0, 10)
	if e != nil || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
	if nulls := nullFields(t, page.Items[0], runFields...); len(nulls) > 0 {
		t.Fatalf("ListAgentRuns sends null for %v", nulls)
	}
	taskFields := []string{"references", "assetIDs"}
	tasks, e := s.GetAgentTasks(run.ID)
	if e != nil || len(tasks) != 1 {
		t.Fatal(tasks, e)
	}
	if nulls := nullFields(t, tasks[0], taskFields...); len(nulls) > 0 {
		t.Fatalf("GetAgentTasks sends null for %v", nulls)
	}
	one, e := s.GetTask(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	if nulls := nullFields(t, one, taskFields...); len(nulls) > 0 {
		t.Fatalf("GetTask sends null for %v", nulls)
	}
	list, e := s.ListTasks("", 0, 10)
	if e != nil || len(list.Items) != 1 {
		t.Fatal(list, e)
	}
	if nulls := nullFields(t, list.Items[0], taskFields...); len(nulls) > 0 {
		t.Fatalf("ListTasks sends null for %v", nulls)
	}
}
