package core

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const maxLogEntries = 5000

type LogEntry struct {
	ID        int64  `json:"id"`
	Time      string `json:"time"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	RelatedID string `json:"relatedID"`
	Message   string `json:"message"`
}
type LogPage struct {
	Items []LogEntry `json:"items"`
	Total int        `json:"total"`
}

var logAuthPattern = regexp.MustCompile(`(?i)(bearer\s+|(?:x-api-key|api[_ -]?key|authorization|token|secret)\s*[=:]\s*["']?)[^\s,;"']+`)
var logURLPattern = regexp.MustCompile(`https?://[^\s"<>]+`)

func (st *Store) safeLogText(text string) string {
	// Compare with saved secrets before persisting. No raw request/response bodies
	// or credential configuration is sent to the log writer.
	connections, _ := List[Connection](st, "connection")
	for _, c := range connections {
		if c.Credential == "" {
			continue
		}
		data, e := readOwned(st.Root, "secrets/"+c.Credential+".bin")
		if e != nil {
			continue
		}
		if plain, e := Unprotect(data); e == nil && len(plain) > 0 {
			text = strings.ReplaceAll(text, string(plain), "[已隐藏]")
		}
	}
	text = logAuthPattern.ReplaceAllString(text, "${1}[已隐藏]")
	text = logURLPattern.ReplaceAllStringFunc(text, func(raw string) string {
		u, e := url.Parse(raw)
		if e != nil {
			return "[地址已隐藏]"
		}
		u.User = nil
		if u.RawQuery != "" {
			u.RawQuery = ""
			u.Fragment = "查询参数已隐藏"
		}
		return u.String()
	})
	runes := []rune(text)
	if len(runes) > 4000 {
		text = string(runes[:4000]) + "…"
	}
	return text
}

func (st *Store) appendLog(level, source, relatedID, message string) error {
	message = st.safeLogText(message)
	tx, e := st.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`INSERT INTO app_logs(time,level,source,related_id,message) VALUES(?,?,?,?,?)`, Now(), level, source, relatedID, message); e != nil {
		return e
	}
	if _, e = tx.Exec(`DELETE FROM app_logs WHERE id <= (SELECT id FROM app_logs ORDER BY id DESC LIMIT 1 OFFSET ?)`, maxLogEntries); e != nil {
		return e
	}
	return tx.Commit()
}

// Only state transitions and diagnostic summaries are logged, never prompts,
// image payloads, URLs of results, Skills, or the Agent's raw thinking output.
func (st *Store) logObjectChange(kind, id string, before, after []byte) {
	level, source, message := "info", "", ""
	switch kind {
	case "task":
		var old, next Task
		json.Unmarshal(before, &old)
		if json.Unmarshal(after, &next) != nil {
			return
		}
		if old.Status == next.Status && old.Error == next.Error && old.Diagnostic.Stage == next.Diagnostic.Stage {
			return
		}
		source = "image"
		message = fmt.Sprintf("生图状态：%s；阶段：%s；模型：%s", next.Status, next.Diagnostic.Stage, next.Model)
		if next.Diagnostic.HTTPStatus != 0 {
			message += fmt.Sprintf("；HTTP %d", next.Diagnostic.HTTPStatus)
		}
		if next.Diagnostic.UpstreamRequestID != "" {
			message += "；供应商请求 ID：" + next.Diagnostic.UpstreamRequestID
		}
		if next.Error != "" {
			message += "；" + next.Error
		}
		if next.Status == "failed" || next.Status == "download_failed" {
			level = "error"
		}
		if next.Status == "unknown" || next.Status == "blocked" || next.Status == "canceled" {
			level = "warn"
		}
	case "agent_run":
		var old, next AgentRun
		json.Unmarshal(before, &old)
		if json.Unmarshal(after, &next) != nil {
			return
		}
		if old.Status == next.Status && old.Stage == next.Stage && old.Error == next.Error && old.Product.Error == next.Product.Error {
			return
		}
		source = "agent"
		message = fmt.Sprintf("智能分析：%s；%s", next.Status, next.Stage)
		if next.Product.Error != "" && next.Product.Error != old.Product.Error {
			message += "；" + next.Product.Error
			level = "warn"
		}
		if next.Error != "" {
			message += "；" + next.Error
		}
		if next.Status == "failed" {
			level = "error"
		}
		if next.Status == "interrupted" || next.Status == "needs_input" {
			level = "warn"
		}
	default:
		return
	}
	// Logging failure must not resend or roll back an already saved paid task.
	_ = st.appendLog(level, source, id, message)
}

func (s *Service) ListLogs(level, source, keyword string, offset, limit int) (LogPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if len([]rune(keyword)) > 200 {
		return LogPage{}, fmt.Errorf("日志关键词最多200个字符")
	}
	where := ` WHERE (?='' OR level=?) AND (?='' OR source=?) AND (?='' OR instr(lower(message || ' ' || related_id),lower(?))>0)`
	args := []any{level, level, source, source, keyword, keyword}
	page := LogPage{Items: []LogEntry{}}
	if e := s.store.db.QueryRow(`SELECT count(*) FROM app_logs`+where, args...).Scan(&page.Total); e != nil {
		return page, e
	}
	rows, e := s.store.db.Query(`SELECT id,time,level,source,related_id,message FROM app_logs`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if e != nil {
		return page, e
	}
	defer rows.Close()
	for rows.Next() {
		var entry LogEntry
		if e = rows.Scan(&entry.ID, &entry.Time, &entry.Level, &entry.Source, &entry.RelatedID, &entry.Message); e != nil {
			return page, e
		}
		page.Items = append(page.Items, entry)
	}
	return page, rows.Err()
}
