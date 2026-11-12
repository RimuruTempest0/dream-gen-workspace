package core

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A data folder written by 0.7.10 (schema version 1) upgrades in place.
func TestSchemaV2MigratesOldDatabase(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	st, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	// Turn it back into a version 1 database: no indexes, no counts.
	for _, q := range []string{"DROP TRIGGER task_count_insert", "DROP TRIGGER task_count_update", "DROP TRIGGER task_count_delete", "DROP TABLE task_counts", "DROP INDEX task_status", "DROP INDEX task_created", "DROP INDEX task_batch", "DROP INDEX task_trial", "DROP INDEX task_updated", "DROP INDEX task_started", "DROP INDEX task_sample_check", "PRAGMA user_version=1"} {
		if _, e = st.db.Exec(q); e != nil {
			t.Fatal(q, e)
		}
	}
	for i, status := range []string{"succeeded", "succeeded", "failed", "queued"} {
		b, _ := json.Marshal(Task{ID: string(rune('a' + i)), Status: status, Trial: i == 0})
		st.db.Exec(`INSERT INTO objects(kind,id,body,created) VALUES('task',?,?,?)`, string(rune('a'+i)), string(b), Now())
	}
	st.Close()

	for round := 0; round < 2; round++ { // the second open finds nothing to do
		st, e = Open(root)
		if e != nil {
			t.Fatal(e)
		}
		var version int
		st.db.QueryRow("PRAGMA user_version").Scan(&version)
		counts, e := st.taskStatusCounts(false)
		if e != nil || version != 2 || counts["succeeded"] != 2 || counts["failed"] != 1 || counts["queued"] != 1 {
			t.Fatalf("round %d: version %d counts %v %v", round, version, counts, e)
		}
		trial, _ := st.taskStatusCounts(true)
		if trial["succeeded"] != 1 || len(trial) != 1 {
			t.Fatalf("trial counts %v", trial)
		}
		st.Close()
	}
	// A database from a newer app is refused, as before.
	db, _ := sql.Open("sqlite", filepath.Join(root, "app.db"))
	db.Exec("PRAGMA user_version=3")
	db.Close()
	if _, e = Open(root); e == nil || !strings.Contains(e.Error(), "高于此应用支持的版本") {
		t.Fatal("newer database opened", e)
	}
}

func TestTaskCountsFollowEveryWrite(t *testing.T) {
	s := testService(t)
	c := saveConnection(t, s, "http://127.0.0.1:1/v1")
	in := request(c)
	in.Count = 3
	batch, e := s.Submit(in)
	if e != nil {
		t.Fatal(e)
	}
	s.StopBatch(batch)
	counts, _ := s.store.taskStatusCounts(false)
	if counts["canceled"] != 3 || counts["queued"] != 0 {
		t.Fatalf("after stop: %v", counts)
	}
	tasks, _ := List[Task](s.store, "task")
	s.store.Delete("task", tasks[0].ID)
	counts, _ = s.store.taskStatusCounts(false)
	if counts["canceled"] != 2 {
		t.Fatalf("after delete: %v", counts)
	}
	sum, _ := s.Dashboard()
	page, _ := s.ListTasks("canceled", 0, 1)
	all, _ := s.ListTasks("", 0, 50)
	if sum.Tasks != 2 || page.Total != 2 || len(page.Items) != 1 || all.Total != 2 || len(all.Items) != 2 {
		t.Fatalf("summary %+v page %+v all %+v", sum, page, all)
	}
}

func TestWorkingTasksSkipOldHistory(t *testing.T) {
	s := testService(t)
	seedTasks(t, s, 300, false)
	now := Now()
	put := func(t2 Task) { s.store.Put("task", t2.ID, t2) }
	put(Task{ID: "queued", BatchID: "suite", Status: "queued", UpdatedAt: "2020-01-01T00:00:00Z", SamplePending: true})
	put(Task{ID: "old-sample", BatchID: "suite", Status: "succeeded", Sample: true, UpdatedAt: "2020-01-01T00:00:00Z"})
	put(Task{ID: "checking", BatchID: "x", Status: "succeeded", SampleCheck: "checking", UpdatedAt: "2020-01-01T00:00:00Z"})
	put(Task{ID: "recent", BatchID: "y", Status: "succeeded", UpdatedAt: now})
	got, e := workingTasks(s.store, healthWindow)
	if e != nil {
		t.Fatal(e)
	}
	ids := map[string]bool{}
	for _, t2 := range got {
		ids[t2.ID] = true
	}
	if len(got) != 4 || !ids["queued"] || !ids["old-sample"] || !ids["checking"] || !ids["recent"] {
		t.Fatalf("working set %v", ids)
	}
	_ = time.Now
}
