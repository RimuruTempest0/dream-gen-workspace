package core

// Image tasks: submit, list, stop, cancel, retry and the workbench draft.

import (
	"errors"
	"fmt"
	"strings"
)

func active(status string) bool {
	return status == "queued" || status == "submitting" || status == "running" || status == "downloading"
}
func bounds(offset, limit, total int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit < 1 || limit > 100 {
		limit = 24
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return offset, end
}
func (s *Service) Submit(in GenerateInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.submit(in)
}
func (s *Service) submit(in GenerateInput) (string, error) {
	if in.RequestID == "" || len(in.RequestID) > 128 {
		return "", errors.New("请求标识无效")
	}
	if in.Count < 1 || in.Count > 20 {
		return "", errors.New("每条提示词张数应为 1–20")
	}
	if len(in.Prompts) < 1 || len(in.Prompts)*in.Count > 200 {
		return "", errors.New("每批最多 200 个子任务")
	}
	if len(in.References) > 4 {
		return "", errors.New("当前版本最多使用 4 张参考图；模型可能有更小限制")
	}
	var c Connection
	if e := s.store.Get("connection", in.ConnectionID, &c); e != nil {
		return "", e
	}
	if normalizeConnection(c).Kind != "image" {
		return "", errors.New("请选择生图中转站，LLM 连接不能提交图片任务")
	}
	if _, e := s.key(c.Credential); e != nil {
		return "", e
	}
	model := strings.TrimSpace(in.Model)
	if model == "" {
		model = c.Model
	}
	if model == "" {
		return "", errors.New("请选择或填写模型 ID")
	}
	if in.ProjectID != "" {
		var project Project
		if e := s.store.Get("project", in.ProjectID, &project); e != nil {
			return "", e
		}
	}
	for _, id := range in.References {
		var a Asset
		if e := s.store.Get("asset", id, &a); e != nil {
			return "", e
		}
		if _, e := readOwned(s.store.Root, a.Path); e != nil {
			return "", errors.New("参考图文件不可用")
		}
	}
	if len(in.Size) > 64 || len(in.Quality) > 64 {
		return "", errors.New("参数过长")
	}
	batch := NewID()
	tasks := []Task{}
	for _, prompt := range in.Prompts {
		prompt = strings.TrimSpace(prompt)
		if prompt == "" || len(prompt) > 32000 {
			return "", errors.New("提示词为空或过长")
		}
		for n := 0; n < in.Count; n++ {
			tasks = append(tasks, Task{ID: NewID(), BatchID: batch, CreatedAt: Now(), UpdatedAt: Now(), Status: "queued", Prompt: prompt, Model: model, Size: in.Size, Quality: in.Quality, References: append([]string{}, in.References...), ProjectID: in.ProjectID, Connection: c, AssetIDs: []string{}})
		}
	}
	if e := s.ensureCanGenerateLocked(len(tasks), c.ID); e != nil {
		return "", e
	}
	for i := range tasks {
		tasks[i].Trial = s.trialLocked()
	}
	result, e := s.store.Submit(in.RequestID, batch, tasks)
	if e == nil {
		s.signal()
	}
	return result, e
}
func (s *Service) ListTasks(status string, offset, limit int) (Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts, e := s.store.taskStatusCounts(false)
	if e != nil {
		return Page{}, e
	}
	total := counts[status]
	where, args := "", []any{}
	if status == "" {
		total = 0
		for _, n := range counts {
			total += n
		}
	} else {
		where, args = taskStatusExpr+"=?", []any{status}
	}
	a, b := bounds(offset, limit, total)
	page, e := listTasksPage(s.store, where, b-a, a, args...)
	if e != nil {
		return Page{}, e
	}
	for i := range page {
		page[i].Connection = cleanConnection(page[i].Connection)
		page[i].Results = nil
	}
	return Page{page, total}, nil
}
func (s *Service) GetTask(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var t Task
	e := s.store.Get("task", id, &t)
	t.Connection = cleanConnection(t.Connection)
	t.Results = nil
	return t, e
}
func (s *Service) StopBatch(batch string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return e
	}
	for _, t := range tasks {
		if t.BatchID == batch && (t.Status == "queued" || t.Status == "blocked") {
			t.Status = "canceled"
			t.UpdatedAt = Now()
			if e = s.store.Put("task", t.ID, t); e != nil {
				return e
			}
		}
	}
	s.signal()
	return nil
}

const (
	canceledUnsent   = "已取消生成。这张图尚未提交，未计费。"
	canceledInFlight = "已取消生成，不再等待中转站结果。请求此前已发送，中转站可能仍会生成并计费，请以中转站记录为准。"
)

// CancelTask cancels one image: an unsent one is simply dropped; an in-flight
// request is aborted. A result already returned (downloading) is not cancelled.
func (s *Service) CancelTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var t Task
	if e := s.store.Get("task", id, &t); e != nil {
		return e
	}
	switch t.Status {
	case "queued", "blocked":
		// A worker that already claimed it re-reads the status before sending.
		t.Status, t.Error, t.UpdatedAt = "canceled", canceledUnsent, Now()
		if e := s.store.Put("task", t.ID, t); e != nil {
			return e
		}
		_ = s.store.appendLog("info", "image", t.ID, "已取消生成：图片尚未提交")
	case "submitting", "running":
		cancel := s.genCancel[id]
		if cancel == nil {
			return errors.New("这张图正在提交，请稍后再取消")
		}
		s.canceled[id] = true
		cancel()
	case "downloading":
		return errors.New("中转站已返回这张图，正在保存，不能取消")
	default:
		return errors.New("这张图已结束，无需取消")
	}
	s.signal()
	return nil
}

func (s *Service) Retry(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var t Task
	if e := s.store.Get("task", id, &t); e != nil {
		return "", e
	}
	if active(t.Status) {
		return "", errors.New("任务仍在执行，不能重复生成")
	}
	if t.AgentRunID != "" {
		if e := s.retryAgentLocked(t); e != nil {
			return "", e
		}
		s.signal()
		return t.BatchID, nil
	}
	return s.submit(GenerateInput{RequestID: NewID(), ConnectionID: t.Connection.ID, Model: t.Model, Prompts: []string{t.Prompt}, Count: 1, References: t.References, Size: t.Size, Quality: t.Quality, ProjectID: t.ProjectID})
}
func (s *Service) RetryDownload(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var t Task
	if e := s.store.Get("task", id, &t); e != nil {
		return e
	}
	if t.Status != "download_failed" {
		return errors.New("此任务没有待补下载的结果")
	}
	if len(t.Results) == 0 {
		return errors.New("没有保留可下载的结果链接，请核对平台记录；不会自动重新生成")
	}
	t.Status = "downloading"
	t.Error = ""
	t.DownloadRetryAt = ""
	if e := s.store.Put("task", id, t); e != nil {
		return e
	}
	s.signal()
	return nil
}
func (s *Service) ResumeBlocked(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var t Task
	if e := s.store.Get("task", id, &t); e != nil {
		return e
	}
	if e := s.resumeBlockedLocked(t); e != nil {
		return e
	}
	s.signal()
	return nil
}

// retryAgentLocked queues a new attempt for a smart-generation slot with the
// connection's current key. It is a new, possibly billable request.
func (s *Service) retryAgentLocked(t Task) error {
	var c Connection
	if e := s.store.Get("connection", t.Connection.ID, &c); e != nil {
		return missing(e, "这张图片使用的生图中转站已删除，请新建需求")
	}
	// A manual retry starts the automatic resends after a content refusal afresh.
	t.PolicyStep = 0
	_, e := s.queueAttemptLocked(t, c, "")
	return e
}

// queueAttemptLocked queues a new attempt of t in the same batch on connection c.
// autoRetryOf marks an automatic re-generation after a relay timeout.
func (s *Service) queueAttemptLocked(t Task, c Connection, autoRetryOf string) (Task, error) {
	if normalizeConnection(c).Kind != "image" {
		return Task{}, errors.New("请选择生图中转站")
	}
	if _, e := s.key(c.Credential); e != nil {
		return Task{}, e
	}
	t.ID = NewID()
	t.CreatedAt = Now()
	t.UpdatedAt = Now()
	t.Status = "queued"
	t.Error = ""
	t.Results = nil
	t.AssetIDs = []string{}
	t.Diagnostic = GenerationDiagnostic{}
	t.PreviousAssetIDs = nil
	t.RoutedFrom, t.RoutedFromName, t.RouteReason = "", "", ""
	t.AutoRetryOf, t.AutoRetriedAs = autoRetryOf, ""
	t.PolicyRetryOf, t.PolicyRetriedAs, t.Rewording, t.RewordFor = "", "", false, ""
	t.SampleCheck, t.SampleProblems = "", ""
	t.Connection = c
	if e := s.ensureCanGenerateLocked(1, c.ID); e != nil {
		return Task{}, e
	}
	t.Trial = s.trialLocked()
	_, e := s.store.Submit(NewID(), t.BatchID, []Task{t})
	return t, e
}

// resumeBlockedLocked puts an unsent, paused task back in the queue.
func (s *Service) resumeBlockedLocked(t Task) error {
	if t.Status != "blocked" {
		return errors.New("只有未提交的暂停任务可恢复")
	}
	if e := s.ensureCanGenerateLocked(1, t.Connection.ID); e != nil {
		return e
	}
	var c Connection
	if e := s.store.Get("connection", t.Connection.ID, &c); e != nil {
		return missing(e, "这张图片使用的生图中转站已删除，请新建需求")
	}
	if c.BaseURL != t.Connection.BaseURL {
		return errors.New("连接地址已修改，请先恢复原地址，或明确重新生成")
	}
	if _, e := s.key(c.Credential); e != nil {
		return e
	}
	t.Connection.Credential = c.Credential
	t.Status = "queued"
	t.Error = ""
	t.UpdatedAt = Now()
	return s.store.Put("task", t.ID, t)
}

// RetryFailedAgentImages regenerates every failed image and resumes every paused
// image of a smart-generation set in one confirmed action. Images whose result is
// unknown are skipped: they may already have been generated and billed.
func (s *Service) RetryFailedAgentImages(runID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := List[Task](s.store, "task")
	if e != nil {
		return 0, e
	}
	seen := map[string]bool{}
	retried := 0
	for _, t := range all { // newest first: the first task of a slot is its current attempt
		if t.AgentRunID != runID || seen[t.PlanItemID] {
			continue
		}
		seen[t.PlanItemID] = true
		switch t.Status {
		case "failed":
			e = s.retryAgentLocked(t)
		case "blocked":
			e = s.resumeBlockedLocked(t)
		default:
			continue
		}
		if e != nil {
			return retried, e
		}
		retried++
	}
	if retried == 0 {
		return 0, errors.New("没有需要重新生成的失败或暂停图片")
	}
	_ = s.store.appendLog("info", "image", runID, fmt.Sprintf("重新生成全部失败图片：%d 张", retried))
	s.signal()
	return retried, nil
}
func (s *Service) Dashboard() (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts, e := s.store.taskStatusCounts(false)
	if e != nil {
		return Summary{}, e
	}
	r := Summary{DataDir: s.store.Root, Version: AppVersion}
	if s.agentRunID != "" {
		r.Active++
	}
	for status, n := range counts {
		r.Tasks += n
		if active(status) {
			r.Active += n
		}
	}
	e = s.store.db.QueryRow(`SELECT COUNT(*) FROM objects WHERE kind='asset' AND COALESCE(json_extract(body,'$.hidden'),0)=0`).Scan(&r.Assets)
	return r, e
}
func (s *Service) SetDraft(v GenerateInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.Put("draft", "workbench", v)
}
func (s *Service) GetDraft() (GenerateInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v GenerateInput
	e := s.store.Get("draft", "workbench", &v)
	if e != nil {
		return GenerateInput{Count: 1, Size: "auto", Quality: "auto", References: []string{}, Prompts: []string{}}, nil
	}
	v.RequestID = ""
	return v, nil
}
