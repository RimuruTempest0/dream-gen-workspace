package core

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogsRecordTransitionsAndRedactBeforePersistence(t *testing.T) {
	s := testService(t)
	c := saveConnection(t, s, "https://example.com/v1")
	s.store.Get("connection", c.ID, &c)
	key, e := s.key(c.Credential)
	if e != nil {
		t.Fatal(e)
	}
	task := Task{ID: "task-log", Status: "failed", Model: "qa-image", Error: "连接失败：" + key + "；https://user:password@example.com/image?signature=private-value", Prompt: "PRIVATE-PROMPT", Results: []Output{{Base64: "PRIVATE-IMAGE-PAYLOAD"}}}
	if e = s.store.Put("task", task.ID, task); e != nil {
		t.Fatal(e)
	}
	if e = s.store.Put("task", task.ID, task); e != nil {
		t.Fatal(e)
	}
	page, e := s.ListLogs("error", "image", "task-log", 0, 50)
	if e != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
	var persisted string
	s.store.db.QueryRow(`SELECT message FROM app_logs WHERE related_id='task-log'`).Scan(&persisted)
	for _, private := range []string{key, "password", "private-value", "PRIVATE-PROMPT", "PRIVATE-IMAGE-PAYLOAD"} {
		if strings.Contains(persisted, private) {
			t.Fatalf("private value persisted: %s", private)
		}
	}
	if !strings.Contains(persisted, "已隐藏") {
		t.Fatal("redaction missing")
	}
	run := AgentRun{ID: "agent-log", Status: "analyzing", Stage: "读取参考图"}
	s.store.Put("agent_run", run.ID, run)
	run.Stage = "等待 LLM 响应"
	s.store.Put("agent_run", run.ID, run)
	page, e = s.ListLogs("info", "agent", "", 0, 1)
	if e != nil || page.Total != 2 || len(page.Items) != 1 || !strings.Contains(page.Items[0].Message, "等待 LLM") {
		t.Fatal(page, e)
	}
	page, _ = s.ListLogs("", "agent", "' OR 1=1 --", 0, 50)
	if page.Total != 0 {
		t.Fatal("keyword was interpreted as SQL")
	}
	page, _ = s.ListLogs("", "agent", "读取参考图", -1, 0)
	if len(page.Items) != 1 {
		t.Fatal("keyword/pagination bounds failed", page)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"unsaved-key-secret denied"}}`))
	}))
	defer up.Close()
	s.TestConnection(ConnectionInput{BaseURL: up.URL, Key: "unsaved-key-secret"})
	page, _ = s.ListLogs("", "connection", "", 0, 50)
	if page.Total != 1 || strings.Contains(page.Items[0].Message, "unsaved-key-secret") {
		t.Fatal("unsaved test key leaked", page)
	}
}

func TestLogsRetentionAndPersistence(t *testing.T) {
	s := testService(t)
	_, e := s.store.db.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<5000) INSERT INTO app_logs(time,level,source,related_id,message) SELECT '2026-10-13T00:00:00Z','info','app','','old log' FROM n`)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.appendLog("warn", "app", "", "persistent-last-log"); e != nil {
		t.Fatal(e)
	}
	page, e := s.ListLogs("", "", "", 0, 50)
	if e != nil || page.Total != maxLogEntries || page.Items[0].Message != "persistent-last-log" {
		t.Fatal(page.Total, e)
	}
	root := s.store.Root
	s.store.Close()
	s.store, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	page, e = s.ListLogs("warn", "app", "persistent-last-log", 0, 50)
	if e != nil || page.Total != 1 {
		t.Fatal("log not preserved after reopening", page, e)
	}
}

func TestLogsUpgradeDatabaseWithoutChangingConnections(t *testing.T) {
	s := testService(t)
	c := saveConnection(t, s, "https://example.com/v1")
	// Recreate the previous schema, which has no log table.
	if _, e := s.store.db.Exec(`DROP TABLE app_logs`); e != nil {
		t.Fatal(e)
	}
	root := s.store.Root
	s.store.Close()
	var e error
	s.store, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	connections, e := s.ListConnections()
	if e != nil || len(connections) != 1 || connections[0].ID != c.ID || !connections[0].HasKey {
		t.Fatal("upgrade changed connection", connections, e)
	}
	page, e := s.ListLogs("", "", "", 0, 50)
	if e != nil || page.Total != 0 {
		t.Fatal("legacy log table not created empty", page, e)
	}
}

func TestLogsSurviveBackupAndDataMigration(t *testing.T) {
	s := testService(t)
	if e := s.store.appendLog("info", "app", "", "migration-log-marker"); e != nil {
		t.Fatal(e)
	}
	backup := filepath.Join(t.TempDir(), "logs-backup.zip")
	if e := s.CreateBackup(backup); e != nil {
		t.Fatal(e)
	}
	newRoot := filepath.Join(t.TempDir(), "新数据")
	if e := s.ChangeDataDirectory(newRoot, func(string) error { return nil }); e != nil {
		t.Fatal(e)
	}
	page, e := s.ListLogs("", "", "migration-log-marker", 0, 50)
	if e != nil || page.Total != 1 {
		t.Fatal("migration lost logs", page, e)
	}
	if e := s.RestoreBackup(backup); e != nil {
		t.Fatal(e)
	}
	page, e = s.ListLogs("", "", "migration-log-marker", 0, 50)
	if e != nil || page.Total != 1 {
		t.Fatal("backup lost logs", page, e)
	}
}
