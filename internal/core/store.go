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
	if version > 1 {
		db.Close()
		return nil, fmt.Errorf("数据库版本 %d 高于此应用支持的版本，请使用更新的软件", version)
	}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS objects(kind TEXT NOT NULL,id TEXT NOT NULL,body TEXT NOT NULL,created TEXT NOT NULL,PRIMARY KEY(kind,id)); CREATE TABLE IF NOT EXISTS submissions(id TEXT PRIMARY KEY,batch TEXT NOT NULL); CREATE TABLE IF NOT EXISTS app_logs(id INTEGER PRIMARY KEY AUTOINCREMENT,time TEXT NOT NULL,level TEXT NOT NULL,source TEXT NOT NULL,related_id TEXT NOT NULL,message TEXT NOT NULL); CREATE TABLE IF NOT EXISTS agent_requests(request_id TEXT PRIMARY KEY,run_id TEXT NOT NULL,operation TEXT NOT NULL); PRAGMA user_version=1;`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db, root}, nil
}
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
