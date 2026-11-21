package core

// Image tasks: submit, list, stop, cancel, retry and the workbench draft.

import (
	"errors"
	"strings"
	"unicode/utf8"
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
		return "", s.fail("task.requestID")
	}
	// Mood guide (0.7.26): the prompt may be empty, and a comparison is one prompt
	// drawn once in each rendering.
	var mood moodParts
	var concept *conceptParts
	renderings := []string{""}
	if in.Mood != nil {
		var e error
		if mood, e = resolveMood(*in.Mood); e != nil {
			return "", s.localErr(e)
		}
		if concept, e = resolveConceptChoice(in.Concept); e != nil {
			return "", s.localErr(e)
		}
		if len(in.Prompts) == 0 {
			in.Prompts = []string{""}
		}
		if in.Compare {
			if len(in.Prompts) != 1 {
				return "", s.fail("task.compareOnePrompt")
			}
			in.Count = 1
			renderings = []string{renderingRealistic, renderingPainterly}
		}
	} else if in.Compare {
		return "", s.fail("task.compareNeedsMood")
	} else if in.Concept != nil {
		return "", s.fail("task.conceptNeedsMood")
	}
	if in.Count < 1 || in.Count > 20 {
		return "", s.fail("task.count")
	}
	if len(in.Prompts) < 1 || len(in.Prompts)*in.Count > 200 {
		return "", s.fail("task.batchLimit")
	}
	if len(in.References) > 4 {
		return "", s.fail("task.referenceLimit")
	}
	var c Connection
	if e := s.store.Get("connection", in.ConnectionID, &c); e != nil {
		return "", e
	}
	if normalizeConnection(c).Kind != "image" {
		return "", s.fail("task.needsImageService")
	}
	if _, e := s.key(c.Credential); e != nil {
		return "", e
	}
	model := strings.TrimSpace(in.Model)
	if model == "" {
		model = c.Model
	}
	if model == "" {
		return "", s.fail("task.needsModel")
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
			return "", s.fail("task.referenceUnavailable")
		}
	}
	if len(in.Size) > 64 || len(in.Quality) > 64 {
		return "", s.fail("task.paramsTooLong")
	}
	batch := NewID()
	tasks := []Task{}
	labels := in.ConceptLabels
	if len(labels) != len(in.Prompts) || in.Mood != nil {
		labels = nil // edited after the concept was applied: the labels no longer match
	}
	for i, prompt := range in.Prompts {
		prompt = strings.TrimSpace(prompt)
		concept := ""
		if labels != nil && utf8.RuneCountInString(labels[i]) <= 64 {
			concept = strings.TrimSpace(labels[i])
		}
		if (prompt == "" && in.Mood == nil) || len(prompt) > 32000 {
			return "", s.fail("task.promptLength")
		}
		for _, rendering := range renderings {
			for n := 0; n < in.Count; n++ {
				tasks = append(tasks, Task{ID: NewID(), BatchID: batch, CreatedAt: Now(), UpdatedAt: Now(), Status: "queued", Prompt: prompt, Model: model, Size: in.Size, Quality: in.Quality, References: append([]string{}, in.References...), ProjectID: in.ProjectID, Connection: c, AssetIDs: []string{}, Concept: concept, Rendering: rendering})
			}
		}
	}
	if e := s.ensureCanGenerateLocked(len(tasks), c.ID); e != nil {
		return "", e
	}
	llm := false
	if in.Mood != nil {
		// Same licence rule as the concept stage: an expired or invalid licence
		// blocks the AI 模型; a trial only uses the main line, else the points are appended.
		if msg := s.licenseBlockMessageLocked(); msg != "" {
			return "", errors.New(msg)
		}
		_, _, llm = s.moodLLMLocked(Task{})
		for i := range tasks {
			choice := *in.Mood
			tasks[i].MoodChoice = &choice
			if in.Concept != nil {
				cc := *in.Concept
				tasks[i].ConceptChoice = &cc
			}
			s.prepareMoodTask(&tasks[i], mood, concept, llm)
		}
	}
	lang := s.Language()
	for i := range tasks {
		tasks[i].Trial = s.trialLocked()
		tasks[i].Language = lang
	}
	result, e := s.store.Submit(in.RequestID, batch, tasks)
	if e == nil {
		if in.Mood != nil && result == batch {
			if llm {
				for _, t := range tasks {
					s.startRewordLocked(t.ID)
				}
			} else {
				_ = s.store.appendLog("info", "image", batch, s.msg("task.log.moodAppended"))
			}
		}
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
		page[i] = cleanTask(page[i])
	}
	return Page{page, total}, nil
}
func (s *Service) GetTask(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var t Task
	e := s.store.Get("task", id, &t)
	return cleanTask(t), e
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
		t.Status, t.Error, t.UpdatedAt = "canceled", s.msg("task.canceledUnsent"), Now()
		if e := s.store.Put("task", t.ID, t); e != nil {
			return e
		}
		_ = s.store.appendLog("info", "image", t.ID, s.msg("task.log.canceledUnsent"))
	case "submitting", "running":
		cancel := s.genCancel[id]
		if cancel == nil {
			return s.fail("task.cancelSubmitting")
		}
		s.canceled[id] = true
		cancel()
	case "downloading":
		return s.fail("task.cancelDownloading")
	default:
		return s.fail("task.cancelFinished")
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
		return "", s.fail("task.stillRunning")
	}
	if t.AgentRunID != "" {
		if e := s.retryAgentLocked(t, triggerRetry); e != nil {
			return "", e
		}
		s.signal()
		return t.BatchID, nil
	}
	if t.MoodChoice != nil {
		return s.retryMoodLocked(t)
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
		return s.fail("task.nothingToDownload")
	}
	if len(t.Results) == 0 {
		return s.fail("task.noResultLinks")
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
	if e := s.resumeBlockedLocked(t, triggerResume); e != nil {
		return e
	}
	s.signal()
	return nil
}

// pauseTask holds an unsent image back ("blocked") for reason and records the
// pause; an image already paused keeps its pause, with the newer reason.
func pauseTask(t *Task, reason string) {
	t.Status, t.Error, t.UpdatedAt = "blocked", reason, Now()
	if n := len(t.Pauses); n > 0 && t.Pauses[n-1].ResumedAt == "" {
		t.Pauses[n-1].Reason = reason
		return
	}
	t.Pauses = append(t.Pauses, TaskPause{At: t.UpdatedAt, Reason: reason})
}

// resumeTask records who resumed a paused image. An image paused before pauses
// were recorded gets its pause from what it kept: its reason and last update.
func resumeTask(t *Task, by string) {
	n := len(t.Pauses)
	if n == 0 || t.Pauses[n-1].ResumedAt != "" {
		t.Pauses = append(t.Pauses, TaskPause{At: t.UpdatedAt, Reason: t.Error})
		n++
	}
	t.Pauses[n-1].ResumedAt, t.Pauses[n-1].ResumedBy = Now(), by
}

// Who started an attempt again (Task.Trigger).
const (
	triggerAutoResend   = "auto-resend"   // the app resent it on its own (relay timeout, content check)
	triggerSampleRedraw = "sample-redraw" // the sample check found it wrong and redrew it
	triggerRetry        = "retry"         // the customer regenerated this one image
	triggerRetryAll     = "retry-all"     // the customer's one-click completion of the set
	triggerRefine       = "refine"        // the customer asked for a change
	// Only a paused image's resume (TaskPause.ResumedBy): the customer resumed it.
	triggerResume = "resume"
)

// retryAgentLocked queues a new attempt for a smart-generation slot with the
// connection's current key. It is a new, possibly billable request.
func (s *Service) retryAgentLocked(t Task, trigger string) error {
	var c Connection
	if e := s.store.Get("connection", t.Connection.ID, &c); e != nil {
		return missing(e, s.msg("task.serviceDeleted"))
	}
	// A manual retry starts the automatic resends after a content refusal afresh.
	t.PolicyStep = 0
	t.Language = s.Language() // a new request: the language chosen now
	_, e := s.queueAttemptLocked(t, c, "", trigger)
	return e
}

// queueAttemptLocked queues a new attempt of t in the same batch on connection c.
// autoRetryOf marks an automatic re-generation after a relay timeout; trigger
// records who started the attempt.
func (s *Service) queueAttemptLocked(t Task, c Connection, autoRetryOf, trigger string) (Task, error) {
	if normalizeConnection(c).Kind != "image" {
		return Task{}, s.fail("task.chooseImageService")
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
	t.AutoRetryOf, t.AutoRetriedAs, t.Trigger = autoRetryOf, "", trigger
	t.Pauses = nil
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

// resumeBlockedLocked puts an unsent, paused task back in the queue; by is who
// resumed it (triggerResume or triggerRetryAll).
func (s *Service) resumeBlockedLocked(t Task, by string) error {
	if t.Status != "blocked" {
		return s.fail("task.resumeOnlyBlocked")
	}
	if e := s.ensureCanGenerateLocked(1, t.Connection.ID); e != nil {
		return e
	}
	var c Connection
	if e := s.store.Get("connection", t.Connection.ID, &c); e != nil {
		return missing(e, s.msg("task.serviceDeleted"))
	}
	if c.BaseURL != t.Connection.BaseURL {
		return s.fail("task.addressChanged")
	}
	if _, e := s.key(c.Credential); e != nil {
		return e
	}
	t.Connection.Credential = c.Credential
	resumeTask(&t, by)
	t.Status = "queued"
	t.Error = ""
	t.UpdatedAt = Now()
	return s.store.Put("task", t.ID, t)
}

// RetryFailedAgentImages regenerates every failed and 结果未知 image and resumes
// every paused image of a 套图 in one confirmed action, so the whole set can be
// completed. The customer confirmed once, knowing that a 结果未知 image may
// already have been generated and billed. It is an explicit action, so it is
// not bound by the one automatic resend per image.
func (s *Service) RetryFailedAgentImages(runID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := List[Task](s.store, "task")
	if e != nil {
		return 0, e
	}
	sortNewestFirst(all)
	seen := map[string]bool{}
	retried := 0
	for _, t := range all { // newest first: the first task of a slot is its current attempt
		if t.AgentRunID != runID || seen[t.PlanItemID] {
			continue
		}
		seen[t.PlanItemID] = true
		switch t.Status {
		case "failed", "unknown":
			e = s.retryAgentLocked(t, triggerRetryAll)
		case "blocked":
			e = s.resumeBlockedLocked(t, triggerRetryAll)
		default:
			continue
		}
		if e != nil {
			return retried, e
		}
		retried++
	}
	if retried == 0 {
		return 0, s.fail("task.nothingToRetry")
	}
	_ = s.store.appendLog("info", "image", runID, s.msg("task.log.retriedAll", "n", retried))
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
