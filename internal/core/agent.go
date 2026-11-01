package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		return AgentRuntimeInfo{Version: ClaudeVersion, Message: "内置 Claude Code 缺失，请完整解压 0.6.19 便携包。普通生图仍可使用。"}
	}
	return AgentRuntimeInfo{Available: true, Version: ClaudeVersion, Message: "内置运行时已就绪"}
}
func (s *Service) waitAgent() {
	s.mu.Lock()
	done := s.agentDone
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}
func (s *Service) agentBusy() error {
	if s.agentRunID != "" {
		return errors.New("智能生图正在分析，请先完成或取消")
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
	return r
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
		r.Events = nil
		out = append(out, r)
	}
	return AgentPage{out, len(all)}, nil
}
func (s *Service) GetAgentRun(id string) (AgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r AgentRun
	e := s.store.Get("agent_run", id, &r)
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
		return errors.New("智能生图草稿过长")
	}
	return s.store.Put("draft", "agent", v)
}

func (s *Service) validateAgentInput(in AgentInput) (Connection, []SkillRecord, error) {
	if strings.TrimSpace(in.Instruction) == "" || len(in.Instruction) > 32000 {
		return Connection{}, nil, errors.New("请填写制作需求，最多 32000 字节")
	}
	if len(in.ProductText) > 64000 || in.TargetCount < 1 || in.TargetCount > 200 || len(in.References) > 20 {
		return Connection{}, nil, errors.New("目标张数应为 1–200，分析参考图最多 20 张，资料最多 64000 字节")
	}
	if in.ProductURL != "" {
		if e := validateProductURL(in.ProductURL); e != nil {
			return Connection{}, nil, e
		}
	}
	var llm, imageConn Connection
	if e := s.store.Get("connection", in.LLMConnectionID, &llm); e != nil {
		return llm, nil, missing(e, "所选 LLM 供应商已删除或未选择，请在“更多设置”中重新选择")
	}
	llm = normalizeConnection(llm)
	if llm.Kind != "llm" || llm.Model == "" {
		return llm, nil, errors.New("请选择已设置模型的 LLM 供应商")
	}
	if !llm.Capabilities.Text || !llm.Capabilities.Tools || !llm.Capabilities.Streaming || llm.Capabilities.Model != llm.Model {
		return llm, nil, errors.New("请先在设置中验证 LLM 的文本、工具调用和流式能力")
	}
	if (len(in.References) > 0 || in.ProductURL != "") && !llm.Capabilities.Vision {
		return llm, nil, errors.New("此需求需要图片分析，请先选择通过图片能力验证的 LLM")
	}
	if _, e := s.key(llm.Credential); e != nil {
		return llm, nil, e
	}
	if e := s.store.Get("connection", in.ImageConnectionID, &imageConn); e != nil {
		return llm, nil, missing(e, "所选生图中转站已删除或未选择，请在“更多设置”中重新选择")
	}
	if normalizeConnection(imageConn).Kind != "image" {
		return llm, nil, errors.New("请选择生图中转站")
	}
	if in.ProjectID != "" {
		var p Project
		if e := s.store.Get("project", in.ProjectID, &p); e != nil {
			return llm, nil, missing(e, "所选项目已删除，请在“更多设置”中重新选择")
		}
	}
	for _, id := range in.References {
		var a Asset
		if e := s.store.Get("asset", id, &a); e != nil {
			return llm, nil, missing(e, "有参考素材已删除，请移除后重新添加")
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
			return llm, nil, errors.New("选中的 Skill 已停用")
		}
		slug, _, e := validateSkill(v.Content)
		if e != nil {
			return llm, nil, e
		}
		if slugs[slug] {
			return llm, nil, errors.New("选中 Skills 的 name 重复")
		}
		slugs[slug] = true
		for _, a := range v.Attachments {
			switch strings.ToLower(filepath.Ext(a.Name)) {
			case ".png", ".jpg", ".jpeg", ".webp":
				if !llm.Capabilities.Vision {
					return llm, nil, errors.New("选定的 Skill 包含图片，请先验证 LLM 图片能力")
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
			return "", errors.New("内置 Claude Code 缺失，请完整解压便携包")
		}
	}
	r := AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: "analyzing", Stage: "准备分析", Input: in, LLMConnection: llm, Skills: skills, Questions: []AgentQuestion{}, Plans: []SuitePlan{}, Events: []AgentEvent{}, Product: ProductInfo{ReferenceIDs: []string{}}}
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
		return errors.New("请使用继续制作接口，保留本次授权参数")
	}
	if r.BatchID != "" {
		return errors.New("此方案已提交生图，请创建新的智能生图需求")
	}
	if len(in.Instruction) > 32000 || len(in.ProductText) > 64000 || len(in.References) > 20 {
		return errors.New("补充资料过长或图片过多")
	}
	if in.Instruction != "" {
		r.Instructions = append(r.Instructions, in.Instruction)
		if len(r.Instructions) > 30 {
			return errors.New("本次需求修改过多，请创建新需求")
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
				return errors.New("补充答案过长")
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
	r.Stage = "重新分析与规划"
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

func (s *Service) launchAgent(r AgentRun, instruction string) {
	r.AnalysisStartedAt = Now()
	r.AttemptStartRevision = len(r.Plans)
	r.AnalysisSucceeded = false
	r.CancelRequested = false
	r.LLMReconnects, r.LLMReconnectError = 0, ""
	s.store.Put("agent_run", r.ID, r)
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
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
			if current.CancelRequested || wasCanceled || s.ctx.Err() != nil {
				e = errors.New("制作已取消或超时")
				wasCanceled = true
			}
			if e != nil {
				current.Status = "failed"
				current.Error = redact(e.Error(), "")
				if current.LLMReconnectError != "" {
					current.Error += fmt.Sprintf("（LLM 连接中断 %d 次，最近一次：%s）", current.LLMReconnects, current.LLMReconnectError)
				}
				if wasCanceled {
					current.Status = "interrupted"
					current.Stage = "分析已中断"
					current.Error = "分析已取消或超时，已保存资料与方案，可明确继续"
				}
			} else if current.Error = ""; hasMissing(current.Questions) {
				// The analysis finished: an earlier problem no longer applies.
				current.Status = "needs_input"
				current.Stage = "等待补充资料"
			} else if len(current.Plans) > current.AttemptStartRevision {
				current.AnalysisSucceeded = true
				current.Status = "review"
				current.Stage = "等待确认方案"
				if current.AutoGeneration {
					current.Status = "ready"
					current.Stage = "制作准备已完成"
				}
			} else {
				current.Status = "failed"
				current.Error = "Agent 未保存套图方案，请补充资料或明确继续"
			}
			current.UpdatedAt = Now()
			if current.AutoGeneration && e == nil && current.AnalysisSucceeded && !hasMissing(current.Questions) {
				s.agentRunID = ""
				if err := s.autoSubmitLocked(&current); err != nil {
					current.Status = "failed"
					current.Error = err.Error()
					current.Stage = "制作未完成"
				}
			}
			// Do not overwrite a run saved atomically with its submitted batch.
			if current.BatchID == "" {
				s.store.Put("agent_run", r.ID, current)
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
		return errors.New("此分析任务当前未运行")
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
			r.Stage = stage
			r.Events = append(r.Events, AgentEvent{Now(), stage})
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
		return plan, errors.New("请等待分析结束后编辑方案")
	}
	var r AgentRun
	if e := s.store.Get("agent_run", id, &r); e != nil {
		return plan, e
	}
	if r.BatchID != "" {
		return plan, errors.New("此方案已经提交，不能修改已提交任务")
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
		return errors.New("套图方案应包含 1–200 张图")
	}
	allowed := map[string]bool{}
	for _, id := range append(append([]string{}, r.Input.References...), r.Product.ReferenceIDs...) {
		allowed[id] = true
	}
	samples := 0
	for _, item := range p.Items {
		if item.Sample {
			samples++
		}
	}
	if samples > 1 {
		return errors.New("整套最多一张细节样板图（sample）")
	}
	ids := map[string]bool{}
	for i := range p.Items {
		item := &p.Items[i]
		if item.ID == "" {
			item.ID = NewID()
		}
		if item.Sample && item.UseSample {
			return errors.New("样板图本身不能再照样板图画（sample 与 useSample 不能同时为 true）")
		}
		if item.NoProduct && (item.Sample || item.UseSample) {
			return errors.New("没有商品的图片（noProduct）不能做样板图，也不需要照样板图画")
		}
		if item.UseSample && samples == 0 {
			return errors.New("方案有图片标了 useSample，但没有标 sample 的样板图")
		}
		if item.UseSample && len(item.References) > 3 {
			return errors.New("照样板图画的图片最多选 3 张参考图，第 4 张留给样板图")
		}
		if ids[item.ID] {
			return errors.New("方案图 ID 重复")
		}
		ids[item.ID] = true
		if strings.TrimSpace(item.Prompt) == "" || len(item.Prompt) > 32000 {
			return errors.New("每张图需填写提示词，最多 32000 字节")
		}
		if len(item.References) > 4 {
			return errors.New("每张生图最多 4 张参考图")
		}
		for _, id := range item.References {
			if !allowed[id] {
				return errors.New("方案使用了不属于本次需求的参考图")
			}
			var a Asset
			if e := s.store.Get("asset", id, &a); e != nil {
				return e
			}
			if _, e := readOwned(s.store.Root, a.Path); e != nil {
				return errors.New("参考图文件不可用，请重新上传")
			}
		}
		if item.Size == "" {
			item.Size = r.Input.Size
		}
		if item.Quality == "" {
			item.Quality = r.Input.Quality
		}
		if len(item.Size) > 64 || len(item.Quality) > 64 {
			return errors.New("生图参数过长")
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
		return "", errors.New("此需求已提交，不能重复提交另一版方案")
	}
	if s.agentRunID == id || hasMissing(r.Questions) {
		return "", errors.New("请先完成分析并补充必要资料")
	}
	if requestID == "" || len(requestID) > 128 {
		return "", errors.New("请求标识无效")
	}
	if len(r.Plans) == 0 || r.Plans[len(r.Plans)-1].Revision != revision {
		return "", errors.New("方案已更新，请刷新后确认当前版本")
	}
	p := r.Plans[len(r.Plans)-1]
	if e := s.validatePlan(&r, &p); e != nil {
		return "", e
	}
	var c Connection
	if r.AutoGeneration {
		if !r.AnalysisSucceeded || r.CancelRequested {
			return "", errors.New("本轮制作尚未正常完成")
		}
		if e := s.validateAutoPlan(&r, &p); e != nil {
			return "", e
		}
		c = r.ImageConnection
	} else if e := s.store.Get("connection", r.Input.ImageConnectionID, &c); e != nil {
		return "", e
	}
	if normalizeConnection(c).Kind != "image" {
		return "", errors.New("请选择生图中转站")
	}
	if _, e := s.key(c.Credential); e != nil {
		return "", e
	}
	model := r.Input.ImageModel
	if model == "" {
		model = c.Model
	}
	if model == "" {
		return "", errors.New("请设置生图模型")
	}
	batch := NewID()
	tasks := []Task{}
	sampleItem := ""
	for _, item := range p.Items {
		if item.Sample {
			sampleItem = item.ID
		}
	}
	for _, item := range p.Items {
		prompt := itemPrompt(r, p, item)
		t := Task{ID: NewID(), BatchID: batch, AgentRunID: id, PlanItemID: item.ID, CreatedAt: Now(), UpdatedAt: Now(), Status: "queued", Prompt: prompt, Model: model, Size: item.Size, Quality: item.Quality, References: append([]string{}, item.References...), ProjectID: r.Input.ProjectID, Connection: c, AssetIDs: []string{}, Sample: item.Sample}
		if item.UseSample && sampleItem != "" {
			t.SampleItem, t.SamplePending = sampleItem, true
		}
		tasks = append(tasks, t)
	}
	r.BatchID = batch
	r.SubmittedRevision = revision
	r.Status = "submitted"
	r.Stage = "已提交生图队列"
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
	_ = s.store.appendLog("info", "agent", id, "套图已提交生图队列")
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
	out := []Task{}
	slot := map[string]int{}
	// List is newest first: the first task of a slot is its current attempt, and
	// images of older attempts (retries, refinements) are kept as previous versions.
	for _, t := range all {
		if t.AgentRunID == id {
			if i, ok := slot[t.PlanItemID]; ok {
				out[i].PreviousAssetIDs = append(out[i].PreviousAssetIDs, t.AssetIDs...)
				continue
			}
			slot[t.PlanItemID] = len(out)
			t.Connection = cleanConnection(t.Connection)
			t.Results = nil
			t.PreviousAssetIDs = nil
			out = append(out, t)
		}
	}
	var run AgentRun
	if s.store.Get("agent_run", id, &run) == nil && run.SubmittedRevision > 0 && run.SubmittedRevision <= len(run.Plans) {
		order := map[string]int{}
		for i, item := range run.Plans[run.SubmittedRevision-1].Items {
			order[item.ID] = i
		}
		sort.SliceStable(out, func(i, j int) bool { return order[out[i].PlanItemID] < order[out[j].PlanItemID] })
	}
	return out, e
}

func agentWorkspace(root, id string) string { return filepath.Join(root, "agent-cache", id) }
