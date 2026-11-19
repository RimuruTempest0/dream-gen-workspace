package core

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
)

func (st *Store) agentRequest(id, operation string) (string, error) {
	if strings.TrimSpace(id) == "" || len(id) > 128 {
		return "", errors.New(st.msg("agent.requestInvalid"))
	}
	var run, savedOperation string
	e := st.db.QueryRow(`SELECT run_id,operation FROM agent_requests WHERE request_id=?`, id).Scan(&run, &savedOperation)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	if savedOperation != operation {
		return "", errors.New(st.msg("agent.requestUsed"))
	}
	return run, nil
}

func (st *Store) saveAgentRequest(r AgentRun, id, operation string) error {
	data, e := json.Marshal(r)
	if e != nil {
		return e
	}
	tx, e := st.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var before []byte
	_ = tx.QueryRow(`SELECT body FROM objects WHERE kind='agent_run' AND id=?`, r.ID).Scan(&before)
	if _, e = tx.Exec(`INSERT INTO objects(kind,id,body,created) VALUES('agent_run',?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body`, r.ID, string(data), r.CreatedAt); e != nil {
		return e
	}
	if _, e = tx.Exec(`INSERT INTO agent_requests(request_id,run_id,operation) VALUES(?,?,?)`, id, r.ID, operation); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	st.logObjectChange("agent_run", r.ID, before, data)
	return nil
}

func (s *Service) freezeGeneration(r *AgentRun) error {
	check := r.Input
	// Frozen Skills remain usable after a settings edit or a backup restore.
	check.SkillIDs = nil
	llm, _, e := s.validateAgentInput(check)
	if e != nil {
		return e
	}
	var c Connection
	if e = s.store.Get("connection", r.Input.ImageConnectionID, &c); e != nil {
		return missing(e, s.msg("agent.snapshot.imageGone"))
	}
	if _, e = s.key(c.Credential); e != nil {
		return e
	}
	model := strings.TrimSpace(r.Input.ImageModel)
	if model == "" {
		model = c.Model
	}
	if model == "" {
		return s.fail("agent.imageModel")
	}
	if len(r.Input.Size) > 64 || len(r.Input.Quality) > 64 {
		return s.fail("agent.paramsTooLong")
	}
	r.Input.ImageModel = model
	r.ImageConnection = normalizeConnection(c)
	r.LLMConnection = llm
	r.AutoGeneration = true
	r.AuthorizedCount = r.Input.TargetCount
	if r.SubmissionID == "" {
		r.SubmissionID = "smart-" + r.ID
	}
	return nil
}

func (s *Service) requireConfiguredSnapshot(c Connection) error {
	var live Connection
	if e := s.store.Get("connection", c.ID, &live); e != nil {
		return s.fail("agent.snapshot.connectionGone")
	}
	if live.Credential == "" {
		return s.fail("agent.snapshot.keyGone")
	}
	return nil
}

func (s *Service) StartSmartGeneration(in AgentInput, requestID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, e := s.store.agentRequest(requestID, "start"); e != nil || id != "" {
		return id, e
	}
	if e := s.agentBusy(); e != nil {
		return "", e
	}
	if in.TargetCount == 0 {
		in.TargetCount = 12
	}
	if e := s.ensureCanGenerateLocked(in.TargetCount, in.LLMConnectionID, in.ImageConnectionID); e != nil {
		return "", e
	}
	if in.Ratio == "" {
		in.Ratio = "1:1"
	}
	if in.Size == "" {
		in.Size = "1024x1024"
	}
	if in.Quality == "" {
		in.Quality = "auto"
	}
	if in.ProductURL == "" {
		links := productLinks(in.Instruction)
		if len(links) > 1 {
			return "", s.fail("agent.smart.manyLinks")
		}
		if len(links) == 1 {
			in.ProductURL = links[0]
		}
	}
	in.ProductURL = cleanProductURL(in.ProductURL)
	allSkills, e := List[SkillRecord](s.store, "skill")
	if e != nil {
		return "", e
	}
	in.SkillIDs = []string{}
	for _, skill := range allSkills {
		if skillForRun(skill, in.Category) {
			in.SkillIDs = append(in.SkillIDs, skill.ID)
		}
	}
	shopRules, e := s.enabledShopRulesLocked()
	if e != nil {
		return "", e
	}
	llm, skills, e := s.validateAgentInput(in)
	if e != nil {
		return "", e
	}
	if s.agentRunner == nil {
		if info, e := os.Stat(s.agentExecutable); e != nil || info.IsDir() {
			return "", s.fail("agent.runtime.missing")
		}
	}
	r := AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: "analyzing", Stage: s.msg("agent.stage.preparingMaterial"), StageCode: stagePreparing, Input: in, LLMConnection: llm, Skills: skills, ShopRules: shopRules, Questions: []AgentQuestion{}, Plans: []SuitePlan{}, Events: []AgentEvent{}, Product: ProductInfo{ReferenceIDs: []string{}}}
	if e = s.freezeGeneration(&r); e != nil {
		return "", e
	}
	if in.AutoMode {
		r.AnswerHabits = s.recentManualAnswersLocked()
	}
	if e = s.store.saveAgentRequest(r, requestID, "start"); e != nil {
		return "", e
	}
	_ = s.store.Put("draft", "agent", r.Input)
	s.launchAgent(r, "")
	return r.ID, nil
}

func productLinks(text string) []string {
	links, seen := []string{}, map[string]bool{}
	for _, raw := range productURLPattern.FindAllString(text, -1) {
		raw = cleanProductURL(raw)
		if validateProductURL(raw) == nil && !seen[raw] {
			links = append(links, raw)
			seen[raw] = true
		}
	}
	return links
}

func (s *Service) ContinueSmartGeneration(in AgentContinuation, requestID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op := "continue:" + in.RunID
	if id, e := s.store.agentRequest(requestID, op); e != nil || id != "" {
		return id, e
	}
	var r AgentRun
	if e := s.store.Get("agent_run", in.RunID, &r); e != nil {
		return "", e
	}
	if r.BatchID != "" {
		return r.ID, nil
	}
	if e := s.agentBusy(); e != nil {
		return "", e
	}
	if len(in.Instruction) > 32000 || len(in.ProductText) > 64000 || len(in.References) > 20 {
		return "", s.fail("agent.continue.tooMuch")
	}
	changed := strings.TrimSpace(in.Instruction) != "" || (in.ProductText != "" && in.ProductText != r.Input.ProductText) || (in.References != nil && !reflect.DeepEqual(in.References, r.Input.References))
	if in.ProductText != "" {
		r.Input.ProductText = in.ProductText
	}
	if in.References != nil {
		r.Input.References = in.References
	}
	if strings.TrimSpace(in.Instruction) != "" {
		r.Instructions = append(r.Instructions, in.Instruction)
	}
	if len(r.Instructions) > 30 {
		return "", s.fail("agent.smart.tooManyEdits")
	}
	for i := range r.Questions {
		if r.Questions[i].ID == "product-material" && (len(r.Input.References) > 0 || strings.TrimSpace(r.Input.ProductText) != "") {
			if r.Questions[i].Answer == "" {
				changed = true
			}
			r.Questions[i].Answer = s.msg("agent.smart.materialAdded")
		}
		if answer, ok := in.Answers[r.Questions[i].ID]; ok {
			if len(answer) > 16000 {
				return "", s.fail("agent.continue.answerTooLong")
			}
			if answer != r.Questions[i].Answer {
				changed = true
			}
			r.Questions[i].Answer = answer
		}
	}
	legacyReady := !r.AutoGeneration && r.Status == "review"
	if r.AutoGeneration && (r.LLMConnection.Credential == "" || r.ImageConnection.Credential == "") {
		var llm, image Connection
		if e := s.store.Get("connection", r.Input.LLMConnectionID, &llm); e != nil {
			return "", missing(e, s.msg("agent.snapshot.llmGone"))
		}
		if e := s.store.Get("connection", r.Input.ImageConnectionID, &image); e != nil {
			return "", missing(e, s.msg("agent.snapshot.imageGone"))
		}
		llm = normalizeConnection(llm)
		image = normalizeConnection(image)
		oldLLM, oldImage := normalizeConnection(r.LLMConnection), normalizeConnection(r.ImageConnection)
		if llm.BaseURL != oldLLM.BaseURL || llm.Model != oldLLM.Model || llm.Mode != oldLLM.Mode || llm.AuthMode != oldLLM.AuthMode || llm.MaxOutputTokens != oldLLM.MaxOutputTokens || image.BaseURL != oldImage.BaseURL || image.Protocol != oldImage.Protocol {
			return "", s.fail("agent.snapshot.changed")
		}
	}
	if !r.AutoGeneration || r.LLMConnection.Credential == "" || r.ImageConnection.Credential == "" {
		if e := s.freezeGeneration(&r); e != nil {
			return "", e
		}
	}
	if _, e := s.key(r.LLMConnection.Credential); e != nil {
		return "", e
	}
	if e := s.requireConfiguredSnapshot(r.LLMConnection); e != nil {
		return "", e
	}
	if e := s.requireConfiguredSnapshot(r.ImageConnection); e != nil {
		return "", e
	}
	if _, e := s.key(r.ImageConnection.Credential); e != nil {
		return "", e
	}
	if len(r.Input.References) > 0 && !r.LLMConnection.Capabilities.Vision {
		return "", s.fail("agent.smart.noVision")
	}
	for _, id := range r.Input.References {
		var a Asset
		if e := s.store.Get("asset", id, &a); e != nil {
			return "", e
		}
		if _, e := readOwned(s.store.Root, a.Path); e != nil {
			return "", s.fail("agent.plan.refUnusable")
		}
	}
	r.CancelRequested = false
	r.Error = ""
	// The user continued it: an automatic run may again continue by itself.
	r.AutoContinues = 0
	resumeTimer(&r)
	r.UpdatedAt = Now()
	reuse := !changed && !hasMissing(r.Questions) && (r.AnalysisSucceeded || legacyReady) && len(r.Plans) > 0
	if reuse {
		p := r.Plans[len(r.Plans)-1]
		reuse = s.validateAutoPlan(&r, &p) == nil
	}
	if reuse {
		r.AnalysisSucceeded = true
		r.Status = "ready"
		r.Stage = s.msg("agent.stage.ready")
	} else {
		r.AnalysisSucceeded = false
		r.Status = "analyzing"
		r.Stage = s.msg("agent.stage.processing")
	}
	if e := s.store.saveAgentRequest(r, requestID, op); e != nil {
		return "", e
	}
	if reuse {
		if e := s.autoSubmitLocked(&r); e != nil {
			r.Status = "failed"
			r.Stage = s.msg("agent.stage.unfinished")
			r.Error = e.Error()
			_ = s.store.Put("agent_run", r.ID, r)
			return r.ID, e
		}
		if r.BatchID == "" {
			_ = s.store.Put("agent_run", r.ID, r)
		}
		s.signal()
	} else {
		s.launchAgent(r, in.Instruction)
	}
	return r.ID, nil
}

func (s *Service) validateAutoPlan(r *AgentRun, p *SuitePlan) error {
	if len(p.Items) != r.AuthorizedCount {
		return s.fail("agent.auto.count", "n", r.AuthorizedCount)
	}
	for _, item := range p.Items {
		size, quality := item.Size, item.Quality
		if size == "" {
			size = r.Input.Size
		}
		if quality == "" {
			quality = r.Input.Quality
		}
		if size != r.Input.Size || quality != r.Input.Quality {
			return s.fail("agent.auto.paramsChanged")
		}
	}
	return s.validatePlan(r, p)
}

func (s *Service) autoSubmitLocked(r *AgentRun) error {
	if r.BatchID != "" {
		return nil
	}
	if !r.AutoGeneration || !r.AnalysisSucceeded || r.CancelRequested || hasMissing(r.Questions) {
		return s.fail("agent.auto.notReady")
	}
	if e := s.requireConfiguredSnapshot(r.ImageConnection); e != nil {
		return e
	}
	// An automatic run plans from the instruction alone when nothing else is available.
	if !r.Input.AutoMode && r.Input.ProductURL != "" && (r.Product.Error != "" || r.Product.SourceURL == "") && len(r.Input.References)+len(r.Product.ReferenceIDs) == 0 && strings.TrimSpace(r.Input.ProductText) == "" {
		r.Questions = append(r.Questions, AgentQuestion{ID: "product-material", Label: s.msg("agent.auto.needMaterial"), Required: true})
		r.Status = "needs_input"
		r.Stage = s.msg("agent.stage.needsInput")
		r.AnalysisSucceeded = false
		return nil
	}
	if len(r.Plans) == 0 {
		return s.fail("agent.auto.incomplete")
	}
	p := r.Plans[len(r.Plans)-1]
	if e := s.validateAutoPlan(r, &p); e != nil {
		return e
	}
	if e := s.store.Put("agent_run", r.ID, *r); e != nil {
		return e
	}
	if _, e := s.submitSuiteLocked(r.ID, r.Plans[len(r.Plans)-1].Revision, r.SubmissionID); e != nil {
		return e
	}
	return s.store.Get("agent_run", r.ID, r)
}
