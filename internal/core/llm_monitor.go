package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// LLMCall is one request the app sent to an LLM provider: a smart-generation
// analysis request through the Agent bridge, or a capability test.
type LLMCall struct {
	ID             string `json:"id"`
	At             string `json:"at"`
	Kind           string `json:"kind"` // analysis, test
	RunID          string `json:"runID,omitempty"`
	ConnectionID   string `json:"connectionID"`
	ConnectionName string `json:"connectionName"`
	Model          string `json:"model"`
	Stage          string `json:"stage"`
	Stream         bool   `json:"stream"`
	Retry          bool   `json:"retry"`
	FirstByteMS    int64  `json:"firstByteMS"`
	TotalMS        int64  `json:"totalMS"`
	InputTokens    int64  `json:"inputTokens"`
	OutputTokens   int64  `json:"outputTokens"`
	Status         string `json:"status"` // completed, dropped, error
	HTTPStatus     int    `json:"httpStatus"`
	Error          string `json:"error,omitempty"`
	RequestID      string `json:"requestID,omitempty"`
}

const maxLLMCalls = 5000

// usage reads token counts from a Messages response or stream event; providers
// report input in message_start and output (sometimes input too) in message_delta.
type usage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheReadTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
}

func (c *LLMCall) addUsage(u usage) {
	if in := u.InputTokens + u.CacheReadTokens + u.CacheCreationTokens; in > c.InputTokens {
		c.InputTokens = in
	}
	if u.OutputTokens > c.OutputTokens {
		c.OutputTokens = u.OutputTokens
	}
}

// readStreamUsage takes the usage out of one SSE frame.
func (c *LLMCall) readStreamUsage(frame []byte) {
	for _, line := range strings.Split(string(frame), "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var v struct {
			Usage   usage `json:"usage"`
			Message struct {
				Usage usage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &v) == nil {
			c.addUsage(v.Usage)
			c.addUsage(v.Message.Usage)
		}
	}
}

func (c *LLMCall) readBodyUsage(raw []byte) {
	var v struct {
		Usage usage `json:"usage"`
	}
	if json.Unmarshal(raw, &v) == nil {
		c.addUsage(v.Usage)
	}
}

// waitingStages are set by the bridge itself; the stage worth showing is the
// last step the Agent reported before the request.
var waitingStages = map[string]bool{"等待 LLM 响应": true, "LLM 连接中断，自动重试": true, "LLM 流中断": true}

// newLLMCallLocked starts a record for a request about to be sent.
func (s *Service) newLLMCallLocked(kind, runID string, c Connection, body []byte) LLMCall {
	call := LLMCall{ID: NewID(), At: Now(), Kind: kind, RunID: runID, ConnectionID: c.ID, ConnectionName: c.Name, Model: c.Model}
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if json.Unmarshal(body, &req) == nil {
		if req.Model != "" {
			call.Model = req.Model
		}
		call.Stream = req.Stream
	}
	if runID != "" {
		var r AgentRun
		if s.store.Get("agent_run", runID, &r) == nil {
			for i := len(r.Events) - 1; i >= 0; i-- {
				if !waitingStages[r.Events[i].Text] {
					call.Stage = r.Events[i].Text
					break
				}
			}
			call.Retry = r.LLMReconnects > 0 && s.lastLLMCallDroppedLocked(runID)
		}
	}
	return call
}

func (s *Service) lastLLMCallDroppedLocked(runID string) bool {
	calls, _ := List[LLMCall](s.store, "llm_call")
	for _, c := range calls { // newest first
		if c.RunID == runID {
			return c.Status == "dropped"
		}
	}
	return false
}

// saveLLMCall finishes a record; started is when the request was sent.
func (s *Service) saveLLMCall(call LLMCall, started time.Time) {
	call.TotalMS = time.Since(started).Milliseconds()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store.Put("llm_call", call.ID, call) == nil {
		s.llmCallsSaved++
		if s.llmCallsSaved%100 == 0 {
			_, _ = s.store.db.Exec(`DELETE FROM objects WHERE kind='llm_call' AND id NOT IN (SELECT id FROM objects WHERE kind='llm_call' ORDER BY created DESC LIMIT ?)`, maxLLMCalls)
		}
	}
}

func (s *Service) startLLMCall(kind, runID string, c Connection, body []byte) LLMCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newLLMCallLocked(kind, runID, c, body)
}

// ---- LLM monitor ---------------------------------------------------------------

type LLMProviderStat struct {
	ConnectionID  string `json:"connectionID"`
	Name          string `json:"name"`
	Model         string `json:"model"`
	Deleted       bool   `json:"deleted"`
	CapabilityOK  bool   `json:"capabilityOK"`
	Requests      int    `json:"requests"`
	AnalysisRuns  int    `json:"analysisRuns"`
	Tests         int    `json:"tests"`
	Completed     int    `json:"completed"`
	Dropped       int    `json:"dropped"`
	Errors        int    `json:"errors"`
	MedianFirstMS int64  `json:"medianFirstMS"`
	MedianTotalMS int64  `json:"medianTotalMS"`
	MaxTotalMS    int64  `json:"maxTotalMS"`
	InputTokens   int64  `json:"inputTokens"`
	OutputTokens  int64  `json:"outputTokens"`
}

type LLMDrop struct {
	At        string `json:"at"`
	RunID     string `json:"runID"`
	RunName   string `json:"runName"`
	Stage     string `json:"stage"`
	WaitedMS  int64  `json:"waitedMS"`
	Reason    string `json:"reason"`
	Detail    string `json:"detail"`
	Outcome   string `json:"outcome"`
	Recovered bool   `json:"recovered"`
}

type LLMRunStat struct {
	RunID          string `json:"runID"`
	StartedAt      string `json:"startedAt"`
	Name           string `json:"name"`
	ConnectionName string `json:"connectionName"`
	Requests       int    `json:"requests"`
	InputTokens    int64  `json:"inputTokens"`
	OutputTokens   int64  `json:"outputTokens"`
	Dropped        int    `json:"dropped"`
	Errors         int    `json:"errors"`
	DurationMS     int64  `json:"durationMS"`
	Result         string `json:"result"`
	ResultTone     string `json:"resultTone"` // success, warning, danger
}

type LLMMonitorResult struct {
	Requests     int               `json:"requests"`
	AnalysisRuns int               `json:"analysisRuns"`
	TestRequests int               `json:"testRequests"`
	InputTokens  int64             `json:"inputTokens"`
	OutputTokens int64             `json:"outputTokens"`
	Dropped      int               `json:"dropped"`
	Recovered    int               `json:"recovered"`
	Errors       int               `json:"errors"`
	ErrorNote    string            `json:"errorNote"`
	MedianRunMS  int64             `json:"medianRunMS"`
	Providers    []LLMProviderStat `json:"providers"`
	Drops        []LLMDrop         `json:"drops"`
	Runs         []LLMRunStat      `json:"runs"`
	Calls        []LLMCall         `json:"calls"`
	CallTotal    int               `json:"callTotal"`
	RunNames     map[string]string `json:"runNames"`
}

var localPeer = regexp.MustCompile(`->(127\.0\.0\.1|\[::1\]|localhost):\d+`)

// dropReason explains a cut connection in plain words.
func dropReason(e string) string {
	low := strings.ToLower(e)
	switch {
	case localPeer.MatchString(e):
		return "本机代理断开连接"
	case strings.Contains(low, "timeout") || strings.Contains(low, "deadline"):
		return "等待超时"
	case strings.Contains(low, "forcibly closed") || strings.Contains(low, "reset"):
		return "连接被重置"
	case strings.Contains(low, "eof"):
		return "回复途中连接被关闭"
	}
	return "连接中断"
}

func runName(r AgentRun) string {
	if r.Product.Title != "" {
		return shortText(r.Product.Title, 24)
	}
	return shortText(r.Input.Instruction, 24)
}

func runResult(r AgentRun) (string, string) {
	switch r.Status {
	case "submitted":
		n := r.AuthorizedCount
		if n == 0 && len(r.Plans) > 0 {
			n = len(r.Plans[len(r.Plans)-1].Items)
		}
		return fmt.Sprintf("已提交 %d 张", n), "success"
	case "ready", "review":
		return "方案已保存", "success"
	case "needs_input":
		return "等待补充资料", "warning"
	case "analyzing":
		return "分析中", "warning"
	case "interrupted":
		return "已中断", "danger"
	case "failed":
		return "制作未完成", "danger"
	}
	return r.Status, ""
}

func (s *Service) llmCallsLocked(q MonitorQuery, now time.Time) ([]LLMCall, error) {
	all, e := List[LLMCall](s.store, "llm_call")
	if e != nil {
		return nil, e
	}
	since := monitorSince(q.Days, now)
	out := []LLMCall{}
	for _, c := range all {
		at, e := time.Parse(time.RFC3339Nano, c.At)
		if e != nil || at.Before(since) || (q.ConnectionID != "" && c.ConnectionID != q.ConnectionID) || (q.Source != "" && c.Kind != q.Source) {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out, nil
}

func (s *Service) LLMMonitor(q MonitorQuery) (LLMMonitorResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	calls, e := s.llmCallsLocked(q, time.Now())
	if e != nil {
		return LLMMonitorResult{}, e
	}
	r := LLMMonitorResult{Providers: []LLMProviderStat{}, Drops: []LLMDrop{}, Runs: []LLMRunStat{}, Calls: []LLMCall{}, RunNames: map[string]string{}}
	providers := map[string]*LLMProviderStat{}
	order := []string{}
	provider := func(id, name, model string) *LLMProviderStat {
		if v, ok := providers[id]; ok {
			return v
		}
		v := &LLMProviderStat{ConnectionID: id, Name: name, Model: model, Deleted: true}
		providers[id] = v
		order = append(order, id)
		return v
	}
	cs, _ := List[Connection](s.store, "connection")
	for _, c := range cs {
		if normalizeConnection(c).Kind != "llm" || (q.ConnectionID != "" && c.ID != q.ConnectionID) {
			continue
		}
		v := provider(c.ID, c.Name, c.Model)
		v.Deleted = false
		cap := c.Capabilities
		v.CapabilityOK = cap.Text && cap.Vision && cap.Tools && cap.Streaming
	}
	first := map[string][]int64{}
	total := map[string][]int64{}
	runs := map[string]*LLMRunStat{}
	runOrder := []string{}
	runEnds := map[string]time.Time{}
	providerRuns := map[string]map[string]bool{}
	errorNotes := map[string]int{}
	runCache := map[string]AgentRun{}
	getRun := func(id string) AgentRun {
		if v, ok := runCache[id]; ok {
			return v
		}
		var v AgentRun
		s.store.Get("agent_run", id, &v)
		runCache[id] = v
		return v
	}
	for _, c := range calls {
		v := provider(c.ConnectionID, c.ConnectionName, c.Model)
		v.Requests++
		r.Requests++
		v.InputTokens += c.InputTokens
		v.OutputTokens += c.OutputTokens
		r.InputTokens += c.InputTokens
		r.OutputTokens += c.OutputTokens
		switch c.Status {
		case "completed":
			v.Completed++
			if c.FirstByteMS > 0 {
				first[c.ConnectionID] = append(first[c.ConnectionID], c.FirstByteMS)
			}
			total[c.ConnectionID] = append(total[c.ConnectionID], c.TotalMS)
		case "dropped":
			v.Dropped++
			r.Dropped++
		default:
			v.Errors++
			r.Errors++
			note := "请求失败"
			if c.HTTPStatus != 0 {
				note = fmt.Sprintf("HTTP %d", c.HTTPStatus)
			}
			errorNotes[note]++
		}
		if c.TotalMS > v.MaxTotalMS {
			v.MaxTotalMS = c.TotalMS
		}
		if c.Kind == "test" {
			v.Tests++
			r.TestRequests++
			continue
		}
		if c.RunID == "" {
			continue
		}
		if providerRuns[c.ConnectionID] == nil {
			providerRuns[c.ConnectionID] = map[string]bool{}
		}
		providerRuns[c.ConnectionID][c.RunID] = true
		run, ok := runs[c.RunID]
		if !ok {
			ar := getRun(c.RunID)
			res, tone := runResult(ar)
			run = &LLMRunStat{RunID: c.RunID, Name: runName(ar), ConnectionName: c.ConnectionName, Result: res, ResultTone: tone}
			runs[c.RunID] = run
			runOrder = append(runOrder, c.RunID)
			r.RunNames[c.RunID] = run.Name
		}
		run.Requests++
		run.InputTokens += c.InputTokens
		run.OutputTokens += c.OutputTokens
		if c.Status == "dropped" {
			run.Dropped++
		} else if c.Status != "completed" {
			run.Errors++
		}
		run.StartedAt = c.At // calls are newest first, so this ends at the earliest
		if at, e := time.Parse(time.RFC3339Nano, c.At); e == nil {
			end := at.Add(time.Duration(c.TotalMS) * time.Millisecond)
			if end.After(runEnds[c.RunID]) {
				runEnds[c.RunID] = end
			}
		}
	}
	for id, v := range providers {
		v.AnalysisRuns = len(providerRuns[id])
		v.MedianFirstMS = percentile(first[id], 0.5)
		v.MedianTotalMS = percentile(total[id], 0.5)
	}
	for _, id := range order {
		v := providers[id]
		if v.Deleted && v.Requests == 0 {
			continue
		}
		r.Providers = append(r.Providers, *v)
	}
	sort.SliceStable(r.Providers, func(i, j int) bool { return r.Providers[i].Requests > r.Providers[j].Requests })
	durations := []int64{}
	for _, id := range runOrder {
		run := runs[id]
		if start, e := time.Parse(time.RFC3339Nano, run.StartedAt); e == nil {
			run.DurationMS = runEnds[id].Sub(start).Milliseconds()
			durations = append(durations, run.DurationMS)
		}
		r.Runs = append(r.Runs, *run)
	}
	r.AnalysisRuns = len(r.Runs)
	r.MedianRunMS = percentile(durations, 0.5)
	best := 0
	for note, n := range errorNotes {
		if n > best {
			best, r.ErrorNote = n, note
		}
	}
	// A drop is recovered when a later request of the same run completed.
	for i, c := range calls {
		if c.Status != "dropped" {
			continue
		}
		d := LLMDrop{At: c.At, RunID: c.RunID, Stage: c.Stage, WaitedMS: c.TotalMS, Reason: dropReason(c.Error), Detail: c.Error}
		if c.Kind == "test" {
			d.RunName, d.Outcome = "能力验证", "测试结果为失败"
		} else {
			d.RunName = r.RunNames[c.RunID]
			for _, later := range calls[:i] {
				if later.RunID == c.RunID && later.Status == "completed" {
					d.Recovered = true
					break
				}
			}
			ar := getRun(c.RunID)
			switch {
			case ar.Status == "failed" || ar.Status == "interrupted":
				d.Outcome = "重试后仍失败 · 可继续制作"
			case ar.Status == "analyzing":
				d.Outcome = "正在重试"
			case d.Recovered:
				d.Outcome = "已自动重试 · 制作完成"
			default:
				d.Outcome = "已自动重试"
				d.Recovered = true
			}
		}
		if d.Recovered {
			r.Recovered++
		}
		if len(r.Drops) < 6 {
			r.Drops = append(r.Drops, d)
		}
	}
	filtered := []LLMCall{}
	for _, c := range calls {
		if q.Result == "" || c.Status == q.Result {
			filtered = append(filtered, c)
		}
	}
	r.CallTotal = len(filtered)
	a, b := pageBounds(q.Offset, q.Limit, len(filtered))
	r.Calls = filtered[a:b]
	for _, c := range r.Calls {
		if c.RunID != "" && r.RunNames[c.RunID] == "" {
			r.RunNames[c.RunID] = runName(getRun(c.RunID))
		}
	}
	return r, nil
}

func (s *Service) ExportLLMCalls(q MonitorQuery, path string) (int, error) {
	s.mu.Lock()
	calls, e := s.llmCallsLocked(q, time.Now())
	names := map[string]string{}
	for _, c := range calls {
		if c.RunID != "" && names[c.RunID] == "" {
			var r AgentRun
			s.store.Get("agent_run", c.RunID, &r)
			names[c.RunID] = runName(r)
		}
	}
	s.mu.Unlock()
	if e != nil {
		return 0, e
	}
	rows := [][]string{{"时间", "类型", "需求", "阶段", "LLM 供应商", "模型", "结果", "HTTP 状态", "首次响应秒数", "总秒数", "输入 tokens", "输出 tokens", "供应商请求 ID", "错误"}}
	for _, c := range calls {
		if q.Result != "" && c.Status != q.Result {
			continue
		}
		kind := "智能生图分析"
		if c.Kind == "test" {
			kind = "能力验证"
		}
		status := ""
		if c.HTTPStatus != 0 {
			status = fmt.Sprint(c.HTTPStatus)
		}
		rows = append(rows, []string{localTime(c.At), kind, names[c.RunID], c.Stage, c.ConnectionName, c.Model, resultLabels[c.Status], status, msText(c.FirstByteMS), msText(c.TotalMS), fmt.Sprint(c.InputTokens), fmt.Sprint(c.OutputTokens), c.RequestID, c.Error})
	}
	return writeCSV(path, rows)
}
