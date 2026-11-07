package core

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
)

func (st *Store) agentRequest(id, operation string) (string, error) {
	if strings.TrimSpace(id) == "" || len(id) > 128 {
		return "", errors.New("请求标识无效")
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
		return "", errors.New("请求标识已用于其他操作")
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
		return missing(e, "这次制作使用的生图中转站已删除，请新建需求并选择现有连接")
	}
	if _, e = s.key(c.Credential); e != nil {
		return e
	}
	model := strings.TrimSpace(r.Input.ImageModel)
	if model == "" {
		model = c.Model
	}
	if model == "" {
		return errors.New("请设置生图模型")
	}
	if len(r.Input.Size) > 64 || len(r.Input.Quality) > 64 {
		return errors.New("生图参数过长")
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
		return errors.New("供应商连接已删除，请新建需求选择连接")
	}
	if live.Credential == "" {
		return errors.New("供应商密钥已清除，请先在设置中配置密钥")
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
			return "", errors.New("指令包含多个商品链接，请先选择本次商品链接")
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
		if skill.Enabled {
			in.SkillIDs = append(in.SkillIDs, skill.ID)
		}
	}
	llm, skills, e := s.validateAgentInput(in)
	if e != nil {
		return "", e
	}
	if s.agentRunner == nil {
		if info, e := os.Stat(s.agentExecutable); e != nil || info.IsDir() {
			return "", errors.New("内置 Claude Code 缺失，请完整解压便携包")
		}
	}
	r := AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: "analyzing", Stage: "准备素材", Input: in, LLMConnection: llm, Skills: skills, Questions: []AgentQuestion{}, Plans: []SuitePlan{}, Events: []AgentEvent{}, Product: ProductInfo{ReferenceIDs: []string{}}}
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
		return "", errors.New("补充资料过长或图片过多")
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
		return "", errors.New("修改次数过多，请新建需求")
	}
	for i := range r.Questions {
		if r.Questions[i].ID == "product-material" && (len(r.Input.References) > 0 || strings.TrimSpace(r.Input.ProductText) != "") {
			if r.Questions[i].Answer == "" {
				changed = true
			}
			r.Questions[i].Answer = "已补充商品素材或描述"
		}
		if answer, ok := in.Answers[r.Questions[i].ID]; ok {
			if len(answer) > 16000 {
				return "", errors.New("补充答案过长")
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
			return "", missing(e, "这次制作使用的 LLM 供应商已删除，请新建需求并选择现有连接")
		}
		if e := s.store.Get("connection", r.Input.ImageConnectionID, &image); e != nil {
			return "", missing(e, "这次制作使用的生图中转站已删除，请新建需求并选择现有连接")
		}
		llm = normalizeConnection(llm)
		image = normalizeConnection(image)
		oldLLM, oldImage := normalizeConnection(r.LLMConnection), normalizeConnection(r.ImageConnection)
		if llm.BaseURL != oldLLM.BaseURL || llm.Model != oldLLM.Model || llm.Mode != oldLLM.Mode || llm.AuthMode != oldLLM.AuthMode || llm.MaxOutputTokens != oldLLM.MaxOutputTokens || image.BaseURL != oldImage.BaseURL || image.Protocol != oldImage.Protocol {
			return "", errors.New("连接配置已改变，请新建需求确认新的供应商与模型")
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
		return "", errors.New("此模型未通过图片能力验证")
	}
	for _, id := range r.Input.References {
		var a Asset
		if e := s.store.Get("asset", id, &a); e != nil {
			return "", e
		}
		if _, e := readOwned(s.store.Root, a.Path); e != nil {
			return "", errors.New("参考图不可用，请重新上传")
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
		r.Stage = "制作准备已完成"
	} else {
		r.AnalysisSucceeded = false
		r.Status = "analyzing"
		r.Stage = "处理中"
	}
	if e := s.store.saveAgentRequest(r, requestID, op); e != nil {
		return "", e
	}
	if reuse {
		if e := s.autoSubmitLocked(&r); e != nil {
			r.Status = "failed"
			r.Stage = "制作未完成"
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
		return fmt.Errorf("本次授权%d张，方案必须恰好包含%d张", r.AuthorizedCount, r.AuthorizedCount)
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
			return errors.New("方案修改了已授权的图片参数")
		}
	}
	return s.validatePlan(r, p)
}

func (s *Service) autoSubmitLocked(r *AgentRun) error {
	if r.BatchID != "" {
		return nil
	}
	if !r.AutoGeneration || !r.AnalysisSucceeded || r.CancelRequested || hasMissing(r.Questions) {
		return errors.New("请先补充必要资料并完成制作准备")
	}
	if e := s.requireConfiguredSnapshot(r.ImageConnection); e != nil {
		return e
	}
	// An automatic run plans from the instruction alone when nothing else is available.
	if !r.Input.AutoMode && r.Input.ProductURL != "" && (r.Product.Error != "" || r.Product.SourceURL == "") && len(r.Input.References)+len(r.Product.ReferenceIDs) == 0 && strings.TrimSpace(r.Input.ProductText) == "" {
		r.Questions = append(r.Questions, AgentQuestion{ID: "product-material", Label: "请上传商品参考图，或在商品描述中补充商品资料", Required: true})
		r.Status = "needs_input"
		r.Stage = "等待补充"
		r.AnalysisSucceeded = false
		return nil
	}
	if len(r.Plans) == 0 {
		return errors.New("尚未准备好完整套图，请继续制作")
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
