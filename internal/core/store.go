package core

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

type Store struct {
	db   *sql.DB
	Root string
}

func NewID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func Open(root string) (*Store, error) {
	for _, dir := range []string{"references", "images", "thumbnails", "secrets", "skills", "agent-cache"} {
		if e := os.MkdirAll(filepath.Join(root, dir), 0700); e != nil {
			return nil, e
		}
	}
	db, e := sql.Open("sqlite", filepath.Join(root, "app.db"))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	var version int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		db.Close()
		return nil, e
	}
	if version > schemaVersion {
		db.Close()
		return nil, fmt.Errorf("数据库版本 %d 高于此应用支持的版本，请使用更新的软件", version)
	}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS objects(kind TEXT NOT NULL,id TEXT NOT NULL,body TEXT NOT NULL,created TEXT NOT NULL,PRIMARY KEY(kind,id)); CREATE TABLE IF NOT EXISTS submissions(id TEXT PRIMARY KEY,batch TEXT NOT NULL); CREATE TABLE IF NOT EXISTS app_logs(id INTEGER PRIMARY KEY AUTOINCREMENT,time TEXT NOT NULL,level TEXT NOT NULL,source TEXT NOT NULL,related_id TEXT NOT NULL,message TEXT NOT NULL); CREATE TABLE IF NOT EXISTS agent_requests(request_id TEXT PRIMARY KEY,run_id TEXT NOT NULL,operation TEXT NOT NULL);`)
	if e == nil && version < schemaVersion {
		e = migrateV2(db)
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db, root}, nil
}

// schemaVersion is PRAGMA user_version; an older app refuses a newer database.
const schemaVersion = 2

// migrateV2 (0.7.11) adds indexes on the task fields that scheduling, the trial
// count and the task list filter on, and a per-status task count kept by
// triggers. It runs in one transaction, so an interrupted migration leaves the
// database at version 1 and simply runs again.
func migrateV2(db *sql.DB) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(taskIndexes + taskCountSchema); e != nil {
		return e
	}
	if _, e = tx.Exec(`DELETE FROM task_counts; INSERT INTO task_counts(status,n) SELECT COALESCE(` + taskStatusExpr + `,''),COUNT(*) FROM objects WHERE kind='task' GROUP BY 1;`); e != nil {
		return e
	}
	if _, e = tx.Exec(fmt.Sprintf("PRAGMA user_version=%d;", schemaVersion)); e != nil {
		return e
	}
	return tx.Commit()
}

// taskCountSchema keeps task_counts in step with every write to a task, whichever
// code path makes it.
const taskCountSchema = `
CREATE TABLE IF NOT EXISTS task_counts(status TEXT PRIMARY KEY,n INTEGER NOT NULL);
CREATE TRIGGER IF NOT EXISTS task_count_insert AFTER INSERT ON objects WHEN new.kind='task' BEGIN
	INSERT INTO task_counts(status,n) VALUES(COALESCE(json_extract(new.body,'$.status'),''),1) ON CONFLICT(status) DO UPDATE SET n=n+1;
END;
CREATE TRIGGER IF NOT EXISTS task_count_delete AFTER DELETE ON objects WHEN old.kind='task' BEGIN
	UPDATE task_counts SET n=n-1 WHERE status=COALESCE(json_extract(old.body,'$.status'),'');
END;
CREATE TRIGGER IF NOT EXISTS task_count_update AFTER UPDATE OF body ON objects WHEN new.kind='task' AND COALESCE(json_extract(old.body,'$.status'),'')<>COALESCE(json_extract(new.body,'$.status'),'') BEGIN
	UPDATE task_counts SET n=n-1 WHERE status=COALESCE(json_extract(old.body,'$.status'),'');
	INSERT INTO task_counts(status,n) VALUES(COALESCE(json_extract(new.body,'$.status'),''),1) ON CONFLICT(status) DO UPDATE SET n=n+1;
END;
`

// taskIndexes index fields inside task bodies. Queries must repeat the exact
// expressions and the literal kind='task' for SQLite to use them.
const taskIndexes = `
CREATE INDEX IF NOT EXISTS task_status ON objects(json_extract(body,'$.status'),created DESC,id) WHERE kind='task';
CREATE INDEX IF NOT EXISTS task_created ON objects(created DESC,id) WHERE kind='task';
CREATE INDEX IF NOT EXISTS task_batch ON objects(json_extract(body,'$.batchID')) WHERE kind='task';
CREATE INDEX IF NOT EXISTS task_trial ON objects(json_extract(body,'$.trial'),json_extract(body,'$.status')) WHERE kind='task';
CREATE INDEX IF NOT EXISTS task_updated ON objects(json_extract(body,'$.updatedAt')) WHERE kind='task';
CREATE INDEX IF NOT EXISTS task_started ON objects(json_extract(body,'$.diagnostic.startedAt')) WHERE kind='task';
CREATE INDEX IF NOT EXISTS task_sample_check ON objects(json_extract(body,'$.sampleCheck')) WHERE kind='task';
`

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Put(kind, id string, value any) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	var previous []byte
	if kind == "task" || kind == "agent_run" {
		_ = s.db.QueryRow(`SELECT body FROM objects WHERE kind=? AND id=?`, kind, id).Scan(&previous)
	}
	_, e = s.db.Exec(`INSERT INTO objects(kind,id,body,created) VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body`, kind, id, string(b), Now())
	if e == nil {
		s.logObjectChange(kind, id, previous, b)
	}
	return e
}
func (s *Store) Get(kind, id string, v any) error {
	var b string
	e := s.db.QueryRow(`SELECT body FROM objects WHERE kind=? AND id=?`, kind, id).Scan(&b)
	if e != nil {
		return fmt.Errorf("记录不存在: %w", e)
	}
	return json.Unmarshal([]byte(b), v)
}

// missing replaces a "record not found" error with a message the user can act on.
func missing(e error, message string) error {
	if errors.Is(e, sql.ErrNoRows) {
		return errors.New(message)
	}
	return e
}
func List[T any](s *Store, kind string) ([]T, error) {
	rows, e := s.db.Query(`SELECT body FROM objects WHERE kind=? ORDER BY created DESC,id`, kind)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var b string
		var v T
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Delete(kind, id string) error {
	_, e := s.db.Exec(`DELETE FROM objects WHERE kind=? AND id=?`, kind, id)
	return e
}
func (s *Store) Submit(id, batch string, tasks []Task) (string, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var old string
	e = tx.QueryRow(`SELECT batch FROM submissions WHERE id=?`, id).Scan(&old)
	if e == nil {
		return old, nil
	}
	if e != sql.ErrNoRows {
		return "", e
	}
	for _, t := range tasks {
		b, e := json.Marshal(t)
		if e != nil {
			return "", e
		}
		if _, e = tx.Exec(`INSERT INTO objects(kind,id,body,created) VALUES('task',?,?,?)`, t.ID, string(b), t.CreatedAt); e != nil {
			return "", e
		}
	}
	if _, e = tx.Exec(`INSERT INTO submissions(id,batch) VALUES(?,?)`, id, batch); e != nil {
		return "", e
	}
	return batch, tx.Commit()
}
func (s *Store) Snapshot(path string) error {
	_, e := s.db.Exec(`VACUUM INTO '` + escapeSQL(path) + `'`)
	return e
}
func escapeSQL(s string) string {
	out := ""
	for _, r := range s {
		if r == '\'' {
			out += "''"
		} else {
			out += string(r)
		}
	}
	return out
}

// Task queries that use the task indexes instead of reading every task.

const taskStatusExpr = `json_extract(body,'$.status')`

// listTasksWhere returns the tasks matching where (SQL over the indexed task
// expressions), newest first like List.
func listTasksWhere(s *Store, where string, args ...any) ([]Task, error) {
	return queryTasks(s, `SELECT body FROM objects WHERE kind='task' AND (`+where+`) ORDER BY created DESC,id`, args...)
}

// listTasksPage is one page of listTasksWhere; an empty where pages through all
// tasks in creation order without sorting them.
func listTasksPage(s *Store, where string, limit, offset int, args ...any) ([]Task, error) {
	if where == "" {
		return queryTasks(s, `SELECT body FROM objects INDEXED BY task_created WHERE kind='task' ORDER BY created DESC,id LIMIT ? OFFSET ?`, limit, offset)
	}
	return queryTasks(s, `SELECT body FROM objects WHERE kind='task' AND (`+where+`) ORDER BY created DESC,id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
}

func queryTasks(s *Store, query string, args ...any) ([]Task, error) {
	rows, e := s.db.Query(query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		var b string
		var t Task
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &t); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// taskStatusCounts counts tasks by status (from task_counts); trialOnly counts
// trial images only (through the trial index; a trial has few images).
func (s *Store) taskStatusCounts(trialOnly bool) (map[string]int, error) {
	query := `SELECT status,n FROM task_counts WHERE n>0`
	if trialOnly {
		query = `SELECT ` + taskStatusExpr + `,COUNT(*) FROM objects WHERE kind='task' AND json_extract(body,'$.trial')=1 GROUP BY 1`
	}
	rows, e := s.db.Query(query)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var status sql.NullString
		var n int
		if e = rows.Scan(&status, &n); e != nil {
			return nil, e
		}
		out[status.String] += n
	}
	return out, rows.Err()
}

// workingTasks is what scheduling and gateway health look at: unfinished tasks,
// samples still being checked, everything in the batches of queued tasks (their
// samples), and tasks touched within window (health looks back that far). Older
// finished tasks are never read. The margin covers times saved with a zone offset.
func workingTasks(s *Store, window time.Duration) ([]Task, error) {
	cutoff := time.Now().Add(-window - 15*time.Hour).UTC().Format(time.RFC3339Nano)
	// A union of index lookups; one WHERE with OR would scan every task.
	return listTasksWhere(s, `id IN (
		SELECT id FROM objects INDEXED BY task_status WHERE kind='task' AND `+taskStatusExpr+` IN ('queued','downloading','submitting','running')
		UNION SELECT id FROM objects INDEXED BY task_sample_check WHERE kind='task' AND json_extract(body,'$.sampleCheck')='checking'
		UNION SELECT id FROM objects INDEXED BY task_updated WHERE kind='task' AND json_extract(body,'$.updatedAt')>=?
		UNION SELECT id FROM objects INDEXED BY task_started WHERE kind='task' AND json_extract(body,'$.diagnostic.startedAt')>=?
		UNION SELECT id FROM objects INDEXED BY task_batch WHERE kind='task' AND json_extract(body,'$.batchID') IN (SELECT json_extract(body,'$.batchID') FROM objects INDEXED BY task_status WHERE kind='task' AND `+taskStatusExpr+`='queued'))`, cutoff, cutoff)
}
