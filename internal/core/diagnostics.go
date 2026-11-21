package core

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"imageworkbench/internal/i18n"
	"imageworkbench/internal/update"
)

// A diagnostic package is what the customer sends the provider when something
// goes wrong: recent logs (already redacted when written, redacted again here),
// version information, the licence mode, the lines' names and health, and the
// errors of recent failures. It never holds keys, activation codes, addresses,
// prompts, images, skill text or customer.dat.

type diagnosticSummary struct {
	CreatedAt     string             `json:"createdAt"`
	AppVersion    string             `json:"appVersion"`
	OS            string             `json:"os"`
	Arch          string             `json:"arch"`
	Package       map[string]any     `json:"package,omitempty"` // version.json
	LicenseMode   string             `json:"licenseMode"`
	SoftwareID    string             `json:"softwareID,omitempty"`
	TrialUsed     int                `json:"trialUsed,omitempty"`
	TrialLimit    int                `json:"trialLimit,omitempty"`
	ClaudeVersion string             `json:"claudeVersion"`
	ImageLines    []diagnosticLine   `json:"imageLines"`
	LLMLines      []diagnosticLine   `json:"llmLines"`
	Health        []diagnosticHealth `json:"imageLineHealth"`
	Tasks         map[string]int     `json:"tasksByStatus"`
	Runs          map[string]int     `json:"smartRunsByStatus"`
	Skills        []diagnosticSkill  `json:"skills"`
}

type diagnosticLine struct {
	Name     string `json:"name"`
	Model    string `json:"model"`
	Managed  string `json:"managed,omitempty"`
	Role     string `json:"role,omitempty"` // primary, backup
	HasKey   bool   `json:"hasKey"`
	Verified bool   `json:"verified,omitempty"`
}

type diagnosticSkill struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Version int    `json:"version"`
	Enabled bool   `json:"enabled"`
}

type diagnosticHealth struct {
	Name           string `json:"name"`
	State          string `json:"state"`
	Reason         string `json:"reason,omitempty"`
	CheckedAt      string `json:"checkedAt,omitempty"`
	MedianMS       int64  `json:"medianMS"`
	RecentTimeouts int    `json:"recentTimeouts"`
}

type diagnosticFailure struct {
	Kind       string `json:"kind"` // image, smart
	ID         string `json:"id"`
	At         string `json:"at"`
	Status     string `json:"status"`
	Stage      string `json:"stage,omitempty"`
	Model      string `json:"model,omitempty"`
	Line       string `json:"line,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	RequestID  string `json:"upstreamRequestID,omitempty"`
	Network    string `json:"networkError,omitempty"`
	Error      string `json:"error"`
}

const (
	diagnosticLogLimit     = 2000
	diagnosticFailureLimit = 50
)

// The README is written in the interface language (data.diagnostics.readme).

// ExportDiagnostics writes the diagnostic package to destination. programDir is
// the program's folder, for version.json.
func (s *Service) ExportDiagnostics(destination, programDir string) error {
	s.mu.Lock()
	summary, logs, failures, e := s.collectDiagnosticsLocked(programDir)
	s.mu.Unlock()
	if e != nil {
		return e
	}
	summaryJSON, _ := json.MarshalIndent(summary, "", "  ")
	failuresJSON, _ := json.MarshalIndent(failures, "", "  ")
	if e = writeZipFile(destination, []zipEntry{{"README.txt", []byte(strings.ReplaceAll(s.msg("data.diagnostics.readme"), "\n", "\r\n"))}, {"summary.json", summaryJSON}, {"logs.txt", []byte(logs)}, {"failures.json", failuresJSON}}); e != nil {
		return e
	}
	s.mu.Lock()
	_ = s.store.appendLog("info", "app", "", s.msg("data.diagnostics.exported"))
	s.mu.Unlock()
	return nil
}

func (s *Service) collectDiagnosticsLocked(programDir string) (diagnosticSummary, string, []diagnosticFailure, error) {
	st := s.licenseStatusLocked()
	sum := diagnosticSummary{CreatedAt: Now(), AppVersion: AppVersion, OS: osVersion(), Arch: runtime.GOARCH, LicenseMode: st.Mode, SoftwareID: st.SoftwareID, TrialUsed: st.TrialUsed, TrialLimit: st.TrialLimit, ClaudeVersion: ClaudeVersion, ImageLines: []diagnosticLine{}, LLMLines: []diagnosticLine{}, Health: []diagnosticHealth{}, Tasks: map[string]int{}, Runs: map[string]int{}, Skills: []diagnosticSkill{}}
	if programDir != "" {
		if b, e := os.ReadFile(filepath.Join(programDir, "version.json")); e == nil {
			var v map[string]any
			if json.Unmarshal(update.TrimBOM(b), &v) == nil {
				sum.Package = v
			}
		}
	}
	conns, e := List[Connection](s.store, "connection")
	if e != nil {
		return sum, "", nil, e
	}
	var g GatewaySettings
	_ = s.store.Get("preferences", gatewaySettingsPrefKey, &g)
	role := map[string]string{g.PrimaryID: "primary"}
	for _, id := range g.BackupIDs {
		role[id] = "backup"
	}
	hide := s.redactorLocked(conns, s.Language())
	redact := func(text string) string { return truncateLogText(hide(text)) }
	names := map[string]string{}
	for _, c := range conns {
		names[c.ID] = c.Name
		line := diagnosticLine{Name: c.Name, Model: c.Model, Managed: c.Managed, HasKey: c.Credential != ""}
		if c.Kind == "llm" {
			line.Verified = c.Capabilities.Tools
			sum.LLMLines = append(sum.LLMLines, line)
		} else {
			line.Role = role[c.ID]
			sum.ImageLines = append(sum.ImageLines, line)
		}
	}
	skills, e := List[SkillRecord](s.store, "skill")
	if e != nil {
		return sum, "", nil, e
	}
	for _, v := range skills {
		sum.Skills = append(sum.Skills, diagnosticSkill{Name: v.Name, Source: v.Source, Version: v.Version, Enabled: v.Enabled})
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return sum, "", nil, e
	}
	hc := &healthCache{s: s, tasks: tasks, now: time.Now(), seen: map[string]GatewayHealth{}}
	for _, c := range conns {
		if c.Kind == "llm" {
			continue
		}
		h := hc.get(c)
		sum.Health = append(sum.Health, diagnosticHealth{Name: c.Name, State: h.State, Reason: redact(h.Reason), CheckedAt: h.CheckedAt, MedianMS: h.MedianMS, RecentTimeouts: h.RecentTimeouts})
	}
	failures := []diagnosticFailure{}
	for _, t := range tasks {
		sum.Tasks[t.Status]++
		if t.Status != "failed" && t.Status != "download_failed" && t.Status != "unknown" {
			continue
		}
		failures = append(failures, diagnosticFailure{Kind: "image", ID: t.ID, At: t.UpdatedAt, Status: t.Status, Stage: t.Diagnostic.Stage, Model: t.Model, Line: names[t.Connection.ID], HTTPStatus: t.Diagnostic.HTTPStatus, RequestID: t.Diagnostic.UpstreamRequestID, Network: redact(t.Diagnostic.NetworkError), Error: redact(t.Error)})
	}
	runs, e := List[AgentRun](s.store, "agent_run")
	if e != nil {
		return sum, "", nil, e
	}
	for _, r := range runs {
		sum.Runs[r.Status]++
		if r.Status != "failed" && r.Status != "interrupted" {
			continue
		}
		failures = append(failures, diagnosticFailure{Kind: "smart", ID: r.ID, At: r.UpdatedAt, Status: r.Status, Stage: r.Stage, Model: r.LLMConnection.Model, Line: r.LLMConnection.Name, Error: redact(strings.TrimSpace(r.Error + " " + r.Product.Error))})
	}
	sort.Slice(failures, func(i, j int) bool { return failures[i].At > failures[j].At })
	if len(failures) > diagnosticFailureLimit {
		failures = failures[:diagnosticFailureLimit]
	}
	rows, e := s.store.db.Query(`SELECT time,level,source,related_id,message FROM app_logs ORDER BY id DESC LIMIT ?`, diagnosticLogLimit)
	if e != nil {
		return sum, "", nil, e
	}
	defer rows.Close()
	lines := []string{}
	for rows.Next() {
		var at, level, source, related, message string
		if e = rows.Scan(&at, &level, &source, &related, &message); e != nil {
			return sum, "", nil, e
		}
		message = strings.ReplaceAll(redact(message), "\n", " ")
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s\t%s", at, level, source, related, message))
	}
	if e = rows.Err(); e != nil {
		return sum, "", nil, e
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return sum, strings.Join(lines, "\r\n") + "\r\n", failures, nil
}

// redactorLocked hides what no package sent to the provider may hold: saved
// keys, credentials and query parameters in text (as the logs do) and the
// address of every line in conns. It does not shorten text (the hidden parts
// are replaced by markers); the diagnostic package cuts it like a log line.
func (s *Service) redactorLocked(conns []Connection, lang string) func(string) string {
	// Lines' addresses stay out too: logs keep a URL's host and path.
	hosts := []string{}
	for _, c := range conns {
		if u, e := url.Parse(c.BaseURL); e == nil && u.Host != "" && !slices.Contains(hosts, u.Host) {
			hosts = append(hosts, u.Host)
		}
	}
	secrets := s.store.savedSecrets()
	address := i18n.T(lang, "data.diagnostics.lineAddress")
	return func(text string) string {
		text = redactText(text, secrets, lang)
		for _, h := range hosts {
			text = strings.ReplaceAll(text, h, address)
		}
		return text
	}
}

type zipEntry struct {
	name string
	data []byte
}

// writeZipFile writes the entries, in order, to a new ZIP at destination; a
// failed write leaves destination as it was.
func writeZipFile(destination string, entries []zipEntry) error {
	file, e := os.CreateTemp(filepath.Dir(destination), ".package-*.zip")
	if e != nil {
		return e
	}
	name := file.Name()
	defer os.Remove(name)
	w := zip.NewWriter(file)
	for _, v := range entries {
		var f interface{ Write([]byte) (int, error) }
		if f, e = w.Create(v.name); e == nil {
			_, e = f.Write(v.data)
		}
		if e != nil {
			break
		}
	}
	if closeErr := w.Close(); e == nil {
		e = closeErr
	}
	if closeErr := file.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	_ = os.Remove(destination)
	return os.Rename(name, destination)
}
