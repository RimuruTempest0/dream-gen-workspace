package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ClaudeVersion = "2.1.296"

func (s *Service) SetAgentExecutable(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentExecutable = path
}
func (s *Service) AgentRuntime() AgentRuntimeInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, e := os.Stat(s.agentExecutable)
	if e != nil || info.IsDir() {
		return AgentRuntimeInfo{Version: ClaudeVersion, Message: s.msg("agent.runtime.missingVersion", "version", AppVersion)}
	}
	return AgentRuntimeInfo{Available: true, Version: ClaudeVersion, Message: s.msg("agent.runtime.ready")}
}
func (s *Service) waitAgent() {
	for {
		s.mu.Lock()
		done := s.agentDone
		s.mu.Unlock()
		if done == nil {
			return
		}
		<-done
		// An automatic run may have been continued by itself: wait for that attempt too.
		s.mu.Lock()
		next := s.agentDone
		s.mu.Unlock()
		if next == done {
			return
		}
	}
}
func (s *Service) agentBusy() error {
	if s.agentRunID != "" {
		return s.fail("agent.busy")
	}
	return nil
}
func cleanAgentRun(r AgentRun) AgentRun {
	// Before 0.5.3 a cut LLM stream was saved as the run's error even when the
	// automatic retry succeeded; show it as a retry note instead.
	if r.Error != "" && r.Status != "failed" && r.Status != "interrupted" {
		r.LLMReconnectError, r.Error = r.Error, ""
		r.LLMReconnects = max(r.LLMReconnects, 1)
	}
	r.LLMConnection = cleanConnection(r.LLMConnection)
	r.ImageConnection = cleanConnection(r.ImageConnection)
	// Lists a run stored without them are sent empty, never null: the interface
	// reads their length.
	r.Instructions = orEmpty(r.Instructions)
	r.Input.References, r.Input.SkillIDs = orEmpty(r.Input.References), orEmpty(r.Input.SkillIDs)
	r.Product.ReferenceIDs = orEmpty(r.Product.ReferenceIDs)
	r.Skills, r.Questions, r.Events = orEmpty(r.Skills), orEmpty(r.Questions), orEmpty(r.Events)
	r.Plans = orEmpty(r.Plans)
	for i := range r.Skills {
		r.Skills[i].Attachments = orEmpty(r.Skills[i].Attachments)
	}
	for i := range r.Plans {
		p := &r.Plans[i]
		p.Locked, p.Editable, p.Items = orEmpty(p.Locked), orEmpty(p.Editable), orEmpty(p.Items)
		for j := range p.Items {
			p.Items[j].References = orEmpty(p.Items[j].References)
		}
	}
	return r
}

// cleanTask is a task as it is sent to the interface: the line without its key,
// no result links, and lists empty rather than null.
func cleanTask(t Task) Task {
	t.Connection = cleanConnection(t.Connection)
	t.Results = nil
	t.References, t.AssetIDs = orEmpty(t.References), orEmpty(t.AssetIDs)
	return t
}

// orEmpty is v, or an empty list when v is nil.
func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}
func (s *Service) ListAgentRuns(offset, limit int) (AgentPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := List[AgentRun](s.store, "agent_run")
	if e != nil {
		return AgentPage{}, e
	}
	a, b := bounds(offset, limit, len(all))
	out := []AgentRun{}
	for _, r := range all[a:b] {
		r = cleanAgentRun(r)
		r.Events, r.Activity = []AgentEvent{}, nil
		out = append(out, r)
	}
	return AgentPage{out, len(all)}, nil
}
func (s *Service) GetAgentRun(id string) (AgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r AgentRun
	e := s.store.Get("agent_run", id, &r)
	s.withLiveReply(&r)
	return cleanAgentRun(r), e
}
func (s *Service) GetAgentDraft() (AgentInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v AgentInput
	if s.store.Get("draft", "agent", &v) != nil {
		v = AgentInput{TargetCount: 12, Ratio: "1:1", Size: "1024x1024", Quality: "auto", SkillIDs: []string{"product-analysis", "etsy-suite", "embroidery-rules"}, References: []string{}}
	}
	// A connection deleted after it was chosen would fail the next start with a
	// missing record; fall back to the only remaining connection of that kind.
	v.LLMConnectionID = s.existingConnection(v.LLMConnectionID, "llm")
	if id := s.existingConnection(v.ImageConnectionID, "image"); id != v.ImageConnectionID {
		v.ImageConnectionID, v.ImageModel = id, ""
	}
	if v.ProjectID != "" {
		var p Project
		if s.store.Get("project", v.ProjectID, &p) != nil {
			v.ProjectID = ""
		}
	}
	return v, nil
}

// existingConnection keeps id when it still exists; otherwise it returns the only
// connection of that kind, or "" so the user chooses.
func (s *Service) existingConnection(id, kind string) string {
	var c Connection
	if id != "" && s.store.Get("connection", id, &c) == nil {
		return id
	}
	all, _ := List[Connection](s.store, "connection")
	found := ""
	for _, c := range all {
		if normalizeConnection(c).Kind == kind {
			if found != "" {
				return ""
			}
			found = c.ID
		}
	}
	return found
}
func (s *Service) SetAgentDraft(v AgentInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(v.Instruction) > 32000 || len(v.ProductText) > 64000 || len(v.References) > 20 {
		return s.fail("agent.draft.tooLong")
	}
	return s.store.Put("draft", "agent", v)
}

func (s *Service) validateAgentInput(in AgentInput) (Connection, []SkillRecord, error) {
	if strings.TrimSpace(in.Instruction) == "" || len(in.Instruction) > 32000 {
		return Connection{}, nil, s.fail("agent.input.instruction")
	}
	if len(in.ProductText) > 64000 || in.TargetCount < 1 || in.TargetCount > 200 || len(in.References) > 20 {
		return Connection{}, nil, s.fail("agent.input.limits")
	}
	if in.ProductURL != "" {
		if e := validateProductURL(in.ProductURL); e != nil {
			return Connection{}, nil, e
		}
	}
	var llm, imageConn Connection
	if e := s.store.Get("connection", in.LLMConnectionID, &llm); e != nil {
		return llm, nil, missing(e, s.msg("agent.input.llmMissing"))
	}
	llm = normalizeConnection(llm)
	if llm.Kind != "llm" || llm.Model == "" {
		return llm, nil, s.fail("agent.input.llmModel")
	}
	if !llm.Capabilities.Text || !llm.Capabilities.Tools || !llm.Capabilities.Streaming || llm.Capabilities.Model != llm.Model {
		return llm, nil, s.fail("agent.input.llmUnverified")
	}
	if (len(in.References) > 0 || in.ProductURL != "") && !llm.Capabilities.Vision {
		return llm, nil, s.fail("agent.input.llmVision")
	}
	if _, e := s.key(llm.Credential); e != nil {
		return llm, nil, e
	}
	if e := s.store.Get("connection", in.ImageConnectionID, &imageConn); e != nil {
		return llm, nil, missing(e, s.msg("agent.input.imageMissing"))
	}
	if normalizeConnection(imageConn).Kind != "image" {
		return llm, nil, s.fail("agent.chooseImage")
	}
	if in.ProjectID != "" {
		var p Project
		if e := s.store.Get("project", in.ProjectID, &p); e != nil {
			return llm, nil, missing(e, s.msg("agent.input.projectMissing"))
		}
	}
	for _, id := range in.References {
		var a Asset
		if e := s.store.Get("asset", id, &a); e != nil {
			return llm, nil, missing(e, s.msg("agent.input.referenceMissing"))
		}
		if _, e := readOwned(s.store.Root, a.Path); e != nil {
			return llm, nil, e
		}
	}
	skills := []SkillRecord{}
	slugs := map[string]bool{}
	for _, id := range in.SkillIDs {
		var v SkillRecord
		if e := s.store.Get("skill", id, &v); e != nil {
			return llm, nil, e
		}
		if !v.Enabled {
			return llm, nil, s.fail("agent.input.skillDisabled")
		}
		slug, _, e := validateSkill(v.Content)
		if e != nil {
			return llm, nil, s.localErr(e)
		}
		if slugs[slug] {
			return llm, nil, s.fail("agent.input.skillDuplicate")
		}
		slugs[slug] = true
		for _, a := range v.Attachments {
			switch strings.ToLower(filepath.Ext(a.Name)) {
			case ".png", ".jpg", ".jpeg", ".webp":
				if !llm.Capabilities.Vision {
					return llm, nil, s.fail("agent.input.skillImages")
				}
			}
		}
		skills = append(skills, v)
	}
	return llm, skills, nil
}
func (s *Service) StartAgentAnalysis(in AgentInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.agentBusy(); e != nil {
		return "", e
	}
	if in.ProductURL == "" {
		in.ProductURL = productURLPattern.FindString(in.Instruction)
	}
	in.ProductURL = cleanProductURL(in.ProductURL)
	llm, skills, e := s.validateAgentInput(in)
	if e != nil {
		return "", e
	}
	if s.agentRunner == nil {
		if _, e = os.Stat(s.agentExecutable); e != nil {
			return "", s.fail("agent.runtime.missing")
		}
	}
	shopRules, e := s.enabledShopRulesLocked()
	if e != nil {
		return "", e
	}
	r := AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: "analyzing", Stage: s.msg("agent.stage.preparing"), Input: in, LLMConnection: llm, Skills: skills, ShopRules: shopRules, Questions: []AgentQuestion{}, Plans: []SuitePlan{}, Events: []AgentEvent{}, Product: ProductInfo{ReferenceIDs: []string{}}}
	if e = s.store.Put("agent_run", r.ID, r); e != nil {
		return "", e
	}
	s.store.Put("draft", "agent", in)
	s.launchAgent(r, "")
	return r.ID, nil
}
func (s *Service) ContinueAgentAnalysis(in AgentContinuation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.agentBusy(); e != nil {
		return e
	}
	var r AgentRun
	if e := s.store.Get("agent_run", in.RunID, &r); e != nil {
		return e
	}
	if r.AutoGeneration {
		return s.fail("agent.continue.useContinue")
	}
	if r.BatchID != "" {
		return s.fail("agent.continue.submitted")
	}
	if len(in.Instruction) > 32000 || len(in.ProductText) > 64000 || len(in.References) > 20 {
		return s.fail("agent.continue.tooMuch")
	}
	if in.Instruction != "" {
		r.Instructions = append(r.Instructions, in.Instruction)
		if len(r.Instructions) > 30 {
			return s.fail("agent.continue.tooManyEdits")
		}
	}
	if in.ProductText != "" {
		r.Input.ProductText = in.ProductText
	}
	if in.References != nil {
		r.Input.References = in.References
	}
	for i := range r.Questions {
		if v, ok := in.Answers[r.Questions[i].ID]; ok {
			if len(v) > 16000 {
				return s.fail("agent.continue.answerTooLong")
			}
			r.Questions[i].Answer = v
		}
	}
	llm, _, e := s.validateAgentInput(r.Input)
	if e != nil {
		return e
	}
	r.LLMConnection = llm
	resumeTimer(&r)
	r.Status = "analyzing"
	r.Stage = s.msg("agent.stage.replanning")
	r.Error = ""
	r.UpdatedAt = Now()
	if e = s.store.Put("agent_run", r.ID, r); e != nil {
		return e
	}
	s.launchAgent(r, in.Instruction)
	return nil
}

// resumeTimer adds the time a stopped run waited for the user to WaitedMS when
// the user continues it. The run's last save marks when it stopped.
func resumeTimer(r *AgentRun) {
	switch r.Status {
	case "needs_input", "review", "interrupted", "failed":
		if stopped, e := time.Parse(time.RFC3339Nano, r.UpdatedAt); e == nil {
			if waited := time.Since(stopped).Milliseconds(); waited > 0 {
				r.WaitedMS += waited
			}
		}
	}
}

// launchAgent starts a round the customer asked for, in the interface language
// chosen now; continueAgent goes on with the round in the language it began in.
func (s *Service) launchAgent(r AgentRun, instruction string) {
	r.Language = s.Language()
	s.continueAgent(r, instruction)
}

func (s *Service) continueAgent(r AgentRun, instruction string) {
	r.AnalysisStartedAt = Now()
	r.AttemptStartRevision = len(r.Plans)
	r.AnalysisSucceeded = false
	r.CancelRequested = false
	r.LLMReconnects, r.LLMReconnectError = 0, ""
	r.Progress = progressForAttempt(r)
	r.StopReason = ""
	s.store.Put("agent_run", r.ID, r)
	ctx, cancel := context.WithTimeout(s.ctx, agentAttemptLimit)
	s.agentCancel = cancel
	s.agentDone = make(chan struct{})
	s.agentRunID = r.ID
	done := s.agentDone
	go func() {
		e := s.executeAgent(ctx, r, instruction)
		wasCanceled := ctx.Err() != nil
		cancel()
		s.mu.Lock()
		var current AgentRun
		if s.store.Get("agent_run", r.ID, &current) == nil {
			current.StageCode = ""
			if current.CancelRequested || wasCanceled || s.ctx.Err() != nil {
				e = s.fail("agent.canceled")
				wasCanceled = true
			}
			if e != nil {
				current.Status = "failed"
				current.Error = s.redact(s.errText(e), "")
				if current.LLMReconnectError != "" {
					current.Error += s.msg("agent.reconnects", "n", current.LLMReconnects, "error", current.LLMReconnectError)
				}
				if wasCanceled {
					current.Status = "interrupted"
					current.Stage = s.msg("agent.stage.interrupted")
					current.Error = interruptedMessage(s.Language(), current)
				}
			} else if current.Error = ""; hasMissing(current.Questions) {
				// The analysis finished: an earlier problem no longer applies.
				current.Status = "needs_input"
				current.Stage = s.msg("agent.stage.needsInput")
			} else if len(current.Plans) > current.AttemptStartRevision {
				current.AnalysisSucceeded = true
				current.Status = "review"
				current.Stage = s.msg("agent.stage.review")
				if current.AutoGeneration {
					current.Status = "ready"
					current.Stage = s.msg("agent.stage.ready")
				}
			} else {
				current.Status = "failed"
				current.Error = s.msg("agent.noPlan")
			}
			current.UpdatedAt = Now()
			if current.AutoGeneration && e == nil && current.AnalysisSucceeded && !hasMissing(current.Questions) {
				s.agentRunID = ""
				if err := s.autoSubmitLocked(&current); err != nil {
					current.Status = "failed"
					current.Error = err.Error()
					current.Stage = s.msg("agent.stage.unfinished")
				}
			}
			relaunch := ""
			if !wasCanceled && s.ctx.Err() == nil {
				relaunch = autoContinueLocked(s.Language(), &current)
			}
			// Do not overwrite a run saved atomically with its submitted batch.
			if current.BatchID == "" {
				s.store.Put("agent_run", r.ID, current)
			}
			if relaunch != "" {
				s.agentRunID = ""
				s.agentCancel = nil
				s.continueAgent(current, relaunch)
				close(done)
				s.mu.Unlock()
				s.signal()
				return
			}
		}
		s.agentRunID = ""
		s.agentCancel = nil
		close(done)
		s.mu.Unlock()
		s.signal()
	}()
}
func hasMissing(q []AgentQuestion) bool {
	for _, v := range q {
		if v.Required && strings.TrimSpace(v.Answer) == "" {
			return true
		}
	}
	return false
}
func (s *Service) CancelAgentAnalysis(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentRunID != id || s.agentCancel == nil {
		return s.fail("agent.notRunning")
	}
	var r AgentRun
	if e := s.store.Get("agent_run", id, &r); e != nil {
		return e
	}
	r.CancelRequested = true
	if e := s.store.Put("agent_run", id, r); e != nil {
		return e
	}
	s.agentCancel()
	return nil
}
func (s *Service) updateAgent(id, stage string, fn func(*AgentRun) error) error {
	s.mu.Lock()
	var r AgentRun
	e := s.store.Get("agent_run", id, &r)
	if e == nil {
		if stage != "" {
			r.Stage, r.StageCode = stage, ""
			r.Events = append(r.Events, AgentEvent{At: Now(), Text: stage})
			if len(r.Events) > 100 {
				r.Events = r.Events[len(r.Events)-100:]
			}
		}
		if fn != nil {
			e = fn(&r)
		}
		if e == nil {
			r.UpdatedAt = Now()
			e = s.store.Put("agent_run", id, r)
		}
	}
	s.mu.Unlock()
	s.signal()
	return e
}
func (s *Service) SaveSuitePlan(id string, plan SuitePlan) (SuitePlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentRunID == id {
		return plan, s.fail("agent.plan.waitAnalysis")
	}
	var r AgentRun
	if e := s.store.Get("agent_run", id, &r); e != nil {
		return plan, e
	}
	if r.BatchID != "" {
		return plan, s.fail("agent.plan.submitted")
	}
	if e := s.validatePlan(&r, &plan); e != nil {
		return plan, e
	}
	r.Plans = append(r.Plans, plan)
	r.UpdatedAt = Now()
	return plan, s.store.Put("agent_run", id, r)
}
func (s *Service) validatePlan(r *AgentRun, p *SuitePlan) error {
	if len(p.Items) < 1 || len(p.Items) > 200 {
		return s.fail("agent.plan.count")
	}
	allowed := map[string]bool{}
	for _, id := range runReferenceIDs(*r) {
		allowed[id] = true
	}
	samples := 0
	areas := map[string]bool{}
	for _, item := range p.Items {
		if item.Sample {
			samples++
			area := strings.TrimSpace(item.SampleArea)
			if areas[area] {
				return s.fail("agent.plan.sampleAreaDuplicate", "area", area)
			}
			areas[area] = true
		}
	}
	if samples > maxSamples {
		return s.fail("agent.plan.sampleMax", "n", maxSamples)
	}
	if samples > 1 && areas[""] {
		return s.fail("agent.plan.sampleAreaRequired")
	}
	ids := map[string]bool{}
	for i := range p.Items {
		item := &p.Items[i]
		if item.ID == "" {
			item.ID = NewID()
		}
		uses := item.UseSample || len(item.UseSamples) > 0
		if item.Sample && uses {
			return s.fail("agent.plan.sampleUsesSample")
		}
		if item.NoProduct && (item.Sample || uses) {
			return s.fail("agent.plan.noProductSample")
		}
		if uses && samples == 0 {
			return s.fail("agent.plan.noSample")
		}
		for _, area := range item.UseSamples {
			if !areas[strings.TrimSpace(area)] {
				return s.fail("agent.plan.unknownSampleArea", "area", area)
			}
		}
		if n := len(samplesUsed(*p, *item)); n > 0 && len(item.References)+n > 4 {
			return s.fail("agent.plan.sampleRefs", "n", n)
		}
		if ids[item.ID] {
			return s.fail("agent.plan.duplicateID")
		}
		ids[item.ID] = true
		if strings.TrimSpace(item.Prompt) == "" || len(item.Prompt) > 32000 {
			return s.fail("agent.plan.prompt")
		}
		if len(item.References) > 4 {
			return s.fail("agent.plan.refs")
		}
		for _, id := range item.References {
			if !allowed[id] {
				return s.fail("agent.plan.foreignRef")
			}
			if !p.KeepPerson && s.earlierSuiteImage(id) {
				return s.fail("agent.plan.earlierSuiteImage")
			}
			var a Asset
			if e := s.store.Get("asset", id, &a); e != nil {
				return e
			}
			if _, e := readOwned(s.store.Root, a.Path); e != nil {
				return s.fail("agent.plan.refUnusable")
			}
		}
		if item.Size == "" {
			item.Size = r.Input.Size
		}
		if item.Quality == "" {
			item.Quality = r.Input.Quality
		}
		if len(item.Size) > 64 || len(item.Quality) > 64 {
			return s.fail("agent.paramsTooLong")
		}
	}
	p.Revision = len(r.Plans) + 1
	p.CreatedAt = Now()
	return nil
}
func (s *Service) SubmitSuite(id string, revision int, requestID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.submitSuiteLocked(id, revision, requestID)
}
func (s *Service) submitSuiteLocked(id string, revision int, requestID string) (string, error) {
	var r AgentRun
	if e := s.store.Get("agent_run", id, &r); e != nil {
		return "", e
	}
	if r.BatchID != "" {
		if r.SubmittedRevision == revision {
			return r.BatchID, nil
		}
		return "", s.fail("agent.submit.already")
	}
	if s.agentRunID == id || hasMissing(r.Questions) {
		return "", s.fail("agent.submit.notReady")
	}
	if requestID == "" || len(requestID) > 128 {
		return "", s.fail("agent.requestInvalid")
	}
	if len(r.Plans) == 0 || r.Plans[len(r.Plans)-1].Revision != revision {
		return "", s.fail("agent.submit.stale")
	}
	p := r.Plans[len(r.Plans)-1]
	if e := s.validatePlan(&r, &p); e != nil {
		return "", e
	}
	var c Connection
	if r.AutoGeneration {
		if !r.AnalysisSucceeded || r.CancelRequested {
			return "", s.fail("agent.submit.unfinished")
		}
		if e := s.validateAutoPlan(&r, &p); e != nil {
			return "", e
		}
		c = r.ImageConnection
	} else if e := s.store.Get("connection", r.Input.ImageConnectionID, &c); e != nil {
		return "", e
	}
	if normalizeConnection(c).Kind != "image" {
		return "", s.fail("agent.chooseImage")
	}
	if _, e := s.key(c.Credential); e != nil {
		return "", e
	}
	model := r.Input.ImageModel
	if model == "" {
		model = c.Model
	}
	if model == "" {
		return "", s.fail("agent.imageModel")
	}
	batch := NewID()
	tasks := []Task{}

	for _, item := range p.Items {
		prompt := itemPrompt(r, p, item)
		t := Task{ID: NewID(), BatchID: batch, AgentRunID: id, PlanItemID: item.ID, CreatedAt: Now(), UpdatedAt: Now(), Status: "queued", Prompt: prompt, Model: model, Size: item.Size, Quality: item.Quality, References: append([]string{}, item.References...), ProjectID: r.Input.ProjectID, Connection: c, AssetIDs: []string{}, Sample: item.Sample}
		t.Language = s.Language() // the images and their sample checks finish in the language they were submitted in
		if item.Sample {
			// The rest of the set copies the sample's details: it gets the best quality.
			t.Quality = sampleQuality
			t.SampleArea = strings.TrimSpace(item.SampleArea)
		}
		if used := samplesUsed(p, item); len(used) > 0 {
			t.SampleItems, t.SampleItem, t.SamplePending = used, used[0], true
		}
		tasks = append(tasks, t)
	}
	r.BatchID = batch
	r.SubmittedRevision = revision
	r.Status = "submitted"
	r.Stage = s.msg("agent.stage.submitted")
	r.UpdatedAt = Now()
	if e := s.ensureCanGenerateLocked(len(tasks), c.ID); e != nil {
		return "", e
	}
	for i := range tasks {
		tasks[i].Trial = s.trialLocked()
	}
	e := s.store.submitSuite(requestID, batch, tasks, r)
	if e != nil {
		return "", e
	}
	_ = s.store.appendLog("info", "agent", id, s.msg("agent.log.submitted"))
	s.signal()
	return batch, nil
}
func (s *Store) submitSuite(requestID, batch string, tasks []Task, r AgentRun) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, t := range tasks {
		b, e := json.Marshal(t)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO objects(kind,id,body,created) VALUES('task',?,?,?)", t.ID, string(b), t.CreatedAt); e != nil {
			return e
		}
	}
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE objects SET body=? WHERE kind='agent_run' AND id=?", string(b), r.ID); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO submissions(id,batch) VALUES(?,?)", requestID, batch); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) GetAgentTasks(id string) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := List[Task](s.store, "task")
	sortNewestFirst(all)
	out := []Task{}
	slot := map[string]int{}
	// Newest first: the first task of a slot is its current attempt, and images
	// of older attempts (retries, refinements) are kept as previous versions.
	for _, t := range all {
		if t.AgentRunID == id {
			if i, ok := slot[t.PlanItemID]; ok {
				out[i].PreviousAssetIDs = append(out[i].PreviousAssetIDs, t.AssetIDs...)
				continue
			}
			slot[t.PlanItemID] = len(out)
			t = cleanTask(t)
			t.PreviousAssetIDs = nil
			out = append(out, t)
		}
	}
	var run AgentRun
	if s.store.Get("agent_run", id, &run) == nil {
		if plan, ok := suitePlanOf(run.Plans, run.SubmittedRevision); ok {
			order := map[string]int{}
			for i, item := range plan.Items {
				order[item.ID] = i
			}
			sort.SliceStable(out, func(i, j int) bool { return order[out[i].PlanItemID] < order[out[j].PlanItemID] })
		}
	}
	return out, e
}

func agentWorkspace(root, id string) string { return filepath.Join(root, "agent-cache", id) }

// setAgentStage sets a stage the interface recognises by its code.
func (s *Service) setAgentStage(id, code, stage string) error {
	return s.updateAgent(id, stage, func(r *AgentRun) error {
		r.StageCode = code
		if n := len(r.Events); n > 0 {
			r.Events[n-1].Code = code
		}
		return nil
	})
}
