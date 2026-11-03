package core

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// The monitors read the app's own records: image tasks for gateways (each task
// keeps a snapshot of the gateway it was sent to) and llm_call records for LLM
// providers. Nothing here calls a provider.

type MonitorQuery struct {
	Days         int    `json:"days"` // 1 = today, 7, 30; 0 = everything
	ConnectionID string `json:"connectionID"`
	Source       string `json:"source"` // gateway: smart, workbench; LLM: analysis, test
	Result       string `json:"result"`
	Offset       int    `json:"offset"`
	Limit        int    `json:"limit"`
}

type DayCount struct {
	Day    string         `json:"day"`
	Counts map[string]int `json:"counts"`
}

type GatewayStat struct {
	ConnectionID     string `json:"connectionID"`
	Name             string `json:"name"`
	Role             string `json:"role"` // primary, backup, ""
	BackupOrder      int    `json:"backupOrder"`
	Deleted          bool   `json:"deleted"`
	State            string `json:"state"`
	CheckedAt        string `json:"checkedAt,omitempty"`
	ProbeMS          int64  `json:"probeMS"`
	Requests         int    `json:"requests"`
	Succeeded        int    `json:"succeeded"`
	Unknown          int    `json:"unknown"`
	Failed           int    `json:"failed"`
	Running          int    `json:"running"`
	MedianMS         int64  `json:"medianMS"`
	P90MS            int64  `json:"p90MS"`
	MedianDownloadMS int64  `json:"medianDownloadMS"`
	RoutedIn         int    `json:"routedIn"`
	RoutedOut        int    `json:"routedOut"`
	AutoRegenerated  int    `json:"autoRegenerated"`
}

type GatewayCall struct {
	TaskID         string `json:"taskID"`
	At             string `json:"at"`
	ConnectionID   string `json:"connectionID"`
	Name           string `json:"name"`
	Source         string `json:"source"`
	SourceLabel    string `json:"sourceLabel"`
	Model          string `json:"model"`
	Result         string `json:"result"` // succeeded, unknown, failed, running
	DownloadFailed bool   `json:"downloadFailed"`
	FailReason     string `json:"failReason,omitempty"` // balance or auth
	HTTPStatus     int    `json:"httpStatus"`
	GenerationMS   int64  `json:"generationMS"`
	DownloadMS     int64  `json:"downloadMS"`
	RequestID      string `json:"requestID"`
	RoutedFromName string `json:"routedFromName,omitempty"`
	AutoRetry      bool   `json:"autoRetry"`
}

type GatewayMonitorResult struct {
	Total           int           `json:"total"`
	Succeeded       int           `json:"succeeded"`
	Unknown         int           `json:"unknown"`
	Failed          int           `json:"failed"`
	FailedAuth      int           `json:"failedAuth"`
	FailedBalance   int           `json:"failedBalance"`
	Running         int           `json:"running"`
	Rerouted        int           `json:"rerouted"`
	AutoRegenerated int           `json:"autoRegenerated"`
	Gateways        []GatewayStat `json:"gateways"`
	Days            []DayCount    `json:"days"`
	Calls           []GatewayCall `json:"calls"`
	CallTotal       int           `json:"callTotal"`
}

func monitorSince(days int, now time.Time) time.Time {
	if days <= 0 {
		return time.Time{}
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return midnight.AddDate(0, 0, -(days - 1))
}

// chartDays lists the local days drawn in the daily chart, oldest first.
func chartDays(days int, now time.Time) []string {
	n := days
	if n < 7 {
		n = 7
	}
	if n > 30 || days <= 0 {
		n = 30
	}
	out := make([]string, n)
	start := monitorSince(n, now)
	for i := range out {
		out[i] = start.AddDate(0, 0, i).Format("01/02")
	}
	return out
}

func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	i := int(float64(len(sorted)-1) * p)
	return sorted[i]
}

func pageBounds(offset, limit, total int) (int, int) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	return bounds(offset, limit, total)
}

func callResult(t Task) string {
	switch t.Status {
	case "succeeded", "downloading", "download_failed":
		return "succeeded"
	case "unknown":
		return "unknown"
	case "submitting", "running":
		return "running"
	}
	return "failed"
}

// slotLabels maps "run/planItem" to the image's position in its set.
func (s *Service) slotLabelsLocked() map[string]int {
	out := map[string]int{}
	runs, _ := List[AgentRun](s.store, "agent_run")
	for _, r := range runs {
		for _, p := range r.Plans {
			for i, item := range p.Items {
				out[r.ID+"/"+item.ID] = i + 1
			}
		}
	}
	return out
}

func (s *Service) gatewayCallsLocked(q MonitorQuery, now time.Time) ([]GatewayCall, []Task, error) {
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return nil, nil, e
	}
	since := monitorSince(q.Days, now)
	slots := s.slotLabelsLocked()
	calls := []GatewayCall{}
	sent := []Task{}
	for _, t := range tasks {
		at, e := time.Parse(time.RFC3339Nano, t.Diagnostic.StartedAt)
		if e != nil || at.Before(since) { // never sent, or outside the period
			continue
		}
		sent = append(sent, t)
		c := GatewayCall{TaskID: t.ID, At: t.Diagnostic.StartedAt, ConnectionID: t.Connection.ID, Name: t.Connection.Name, Model: t.Model, Result: callResult(t), DownloadFailed: t.Status == "download_failed", HTTPStatus: t.Diagnostic.HTTPStatus, GenerationMS: t.Diagnostic.ElapsedMS, DownloadMS: t.Diagnostic.DownloadMS, RequestID: t.Diagnostic.UpstreamRequestID, RoutedFromName: t.RoutedFromName, AutoRetry: t.AutoRetryOf != ""}
		if c.Result == "failed" {
			c.FailReason = rejectionKind(t.Diagnostic.HTTPStatus, t.Error)
		}
		if t.AgentRunID != "" {
			c.Source = "smart"
			c.SourceLabel = "智能生图"
			if n := slots[t.AgentRunID+"/"+t.PlanItemID]; n > 0 {
				c.SourceLabel = fmt.Sprintf("智能生图 · 第 %d 张", n)
			}
		} else {
			c.Source = "workbench"
			batch := t.BatchID
			if len(batch) > 6 {
				batch = batch[:6]
			}
			c.SourceLabel = "生图工作台 · 批次 " + batch
		}
		if (q.ConnectionID != "" && c.ConnectionID != q.ConnectionID) || (q.Source != "" && c.Source != q.Source) {
			continue
		}
		calls = append(calls, c)
	}
	sort.SliceStable(calls, func(i, j int) bool { return calls[i].At > calls[j].At })
	return calls, sent, nil
}

func (s *Service) GatewayMonitor(q MonitorQuery) (GatewayMonitorResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	calls, tasks, e := s.gatewayCallsLocked(q, now)
	if e != nil {
		return GatewayMonitorResult{}, e
	}
	cs, _ := List[Connection](s.store, "connection")
	g := s.gatewaySettingsLocked()
	stats := map[string]*GatewayStat{}
	order := []string{}
	stat := func(id, name string) *GatewayStat {
		if v, ok := stats[id]; ok {
			return v
		}
		v := &GatewayStat{ConnectionID: id, Name: name, Deleted: true, State: "unknown"}
		stats[id] = v
		order = append(order, id)
		return v
	}
	allTasks, _ := List[Task](s.store, "task")
	for _, c := range cs {
		if normalizeConnection(c).Kind != "image" {
			continue
		}
		v := stat(c.ID, c.Name)
		v.Name, v.Deleted = c.Name, false
		h := s.healthLocked(c, allTasks, now)
		v.State, v.CheckedAt, v.ProbeMS = h.State, h.CheckedAt, h.ProbeMS
		if g.PrimaryID == c.ID {
			v.Role = "primary"
		} else if i := slices.Index(g.BackupIDs, c.ID); i >= 0 {
			v.Role, v.BackupOrder = "backup", i+1
		}
	}
	r := GatewayMonitorResult{Gateways: []GatewayStat{}, Calls: []GatewayCall{}}
	gen := map[string][]int64{}
	dl := map[string][]int64{}
	days := chartDays(q.Days, now)
	dayIndex := map[string]int{}
	for i, d := range days {
		dayIndex[d] = i
		r.Days = append(r.Days, DayCount{Day: d, Counts: map[string]int{}})
	}
	for _, c := range calls {
		v := stat(c.ConnectionID, c.Name)
		v.Requests++
		r.Total++
		switch c.Result {
		case "succeeded":
			v.Succeeded++
			r.Succeeded++
			if c.GenerationMS > 0 {
				gen[c.ConnectionID] = append(gen[c.ConnectionID], c.GenerationMS)
			}
			if c.DownloadMS > 0 {
				dl[c.ConnectionID] = append(dl[c.ConnectionID], c.DownloadMS)
			}
		case "unknown":
			v.Unknown++
			r.Unknown++
		case "running":
			v.Running++
			r.Running++
		default:
			v.Failed++
			r.Failed++
			switch c.FailReason {
			case "auth":
				r.FailedAuth++
			case "balance":
				r.FailedBalance++
			}
		}
		if c.RoutedFromName != "" {
			v.RoutedIn++
			r.Rerouted++
		}
		if c.AutoRetry {
			v.AutoRegenerated++
			r.AutoRegenerated++
		}
		if at, e := time.Parse(time.RFC3339Nano, c.At); e == nil {
			if i, ok := dayIndex[at.Local().Format("01/02")]; ok {
				r.Days[i].Counts[c.ConnectionID]++
			}
		}
	}
	for _, t := range tasks {
		if t.RoutedFrom != "" && (q.ConnectionID == "" || q.ConnectionID == t.RoutedFrom) {
			if v, ok := stats[t.RoutedFrom]; ok {
				v.RoutedOut++
			}
		}
	}
	for _, id := range order {
		v := stats[id]
		if q.ConnectionID != "" && id != q.ConnectionID {
			continue
		}
		if v.Deleted && v.Requests == 0 {
			continue
		}
		v.MedianMS, v.P90MS = percentile(gen[id], 0.5), percentile(gen[id], 0.9)
		v.MedianDownloadMS = percentile(dl[id], 0.5)
		r.Gateways = append(r.Gateways, *v)
	}
	sort.SliceStable(r.Gateways, func(i, j int) bool {
		rank := func(v GatewayStat) int {
			switch v.Role {
			case "primary":
				return 0
			case "backup":
				return v.BackupOrder
			}
			return 100
		}
		if rank(r.Gateways[i]) != rank(r.Gateways[j]) {
			return rank(r.Gateways[i]) < rank(r.Gateways[j])
		}
		return r.Gateways[i].Requests > r.Gateways[j].Requests
	})
	filtered := []GatewayCall{}
	for _, c := range calls {
		if q.Result == "" || c.Result == q.Result {
			filtered = append(filtered, c)
		}
	}
	r.CallTotal = len(filtered)
	a, b := pageBounds(q.Offset, q.Limit, len(filtered))
	r.Calls = filtered[a:b]
	return r, nil
}

var resultLabels = map[string]string{"succeeded": "成功", "unknown": "结果未知", "failed": "失败", "running": "进行中", "completed": "完成", "dropped": "连接中断", "error": "错误"}

func msText(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f", float64(ms)/1000)
}

func localTime(stamp string) string {
	if at, e := time.Parse(time.RFC3339Nano, stamp); e == nil {
		return at.Local().Format("2006-01-02 15:04:05")
	}
	return stamp
}

// writeCSV saves rows with a UTF-8 BOM so Excel shows Chinese text correctly.
func writeCSV(path string, rows [][]string) (int, error) {
	f, e := os.CreateTemp(filepath.Dir(path), ".monitor-*.partial")
	if e != nil {
		return 0, e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	f.WriteString("\xef\xbb\xbf")
	for _, row := range rows {
		for i, cell := range row {
			// Names and errors come from users and providers: never let Excel run them.
			if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
				row[i] = "'" + cell
			}
		}
	}
	w := csv.NewWriter(f)
	if e = w.WriteAll(rows); e != nil {
		return 0, e
	}
	if e = f.Close(); e != nil {
		return 0, e
	}
	return len(rows) - 1, os.Rename(f.Name(), path)
}

func (s *Service) ExportGatewayCalls(q MonitorQuery, path string) (int, error) {
	s.mu.Lock()
	calls, _, e := s.gatewayCallsLocked(q, time.Now())
	s.mu.Unlock()
	if e != nil {
		return 0, e
	}
	rows := [][]string{{"时间", "中转站", "来源", "模型", "结果", "HTTP 状态", "生成秒数", "下载秒数", "中转站请求 ID", "转入自", "自动重新生成", "任务 ID"}}
	for _, c := range calls {
		if q.Result != "" && c.Result != q.Result {
			continue
		}
		result := resultLabels[c.Result]
		if c.DownloadFailed {
			result += "（下载未完成）"
		}
		if c.FailReason != "" {
			result += "（" + rejectionLabels[c.FailReason] + "）"
		}
		status := ""
		if c.HTTPStatus != 0 {
			status = fmt.Sprint(c.HTTPStatus)
		}
		auto := ""
		if c.AutoRetry {
			auto = "是"
		}
		rows = append(rows, []string{localTime(c.At), c.Name, c.SourceLabel, c.Model, result, status, msText(c.GenerationMS), msText(c.DownloadMS), c.RequestID, c.RoutedFromName, auto, c.TaskID})
	}
	return writeCSV(path, rows)
}

func shortText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
