package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Service struct {
	mu                sync.Mutex
	store             *Store
	provider          *Provider
	ctx               context.Context
	cancel            context.CancelFunc
	done              chan struct{}
	wake              chan struct{}
	emit              func()
	paused            bool
	connectionTests   map[string]context.CancelFunc
	agentExecutable   string
	agentCancel       context.CancelFunc
	agentDone         chan struct{}
	agentRunID        string
	agentRunner       func(context.Context, agentInvocation) error
	allowLocalProduct bool
	productBrowser    productBrowserConfig
	browserMu         sync.Mutex
	// imageConcurrency is how many generation tasks run at once (saved preference).
	imageConcurrency int
	inflight         map[string]bool
	workers          sync.WaitGroup
}

const (
	defaultImageConcurrency = 3
	maxImageConcurrency     = 8
)

func (s *Service) ImageConcurrency() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.imageConcurrency
}

// SetImageConcurrency changes the number of parallel generation requests. It
// applies immediately: more tasks start when raised; running ones finish when lowered.
func (s *Service) SetImageConcurrency(n int) error {
	if n < 1 || n > maxImageConcurrency {
		return fmt.Errorf("同时生成张数需在 1–%d 之间", maxImageConcurrency)
	}
	s.mu.Lock()
	if e := s.store.Put("preferences", "image-concurrency", n); e != nil {
		s.mu.Unlock()
		return e
	}
	changed := s.imageConcurrency != n
	s.imageConcurrency = n
	if changed {
		_ = s.store.appendLog("info", "image", "", fmt.Sprintf("同时生成张数调整为 %d", n))
	}
	s.mu.Unlock()
	s.signal()
	return nil
}

func (s *Service) Dispose()     { s.cancel(); s.waitAgent(); s.store.Close() }
func (s *Service) StopForExit() { s.mu.Lock(); s.paused = true; s.mu.Unlock(); s.cancel() }
func (s *Service) StopAllQueued() {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return
	}
	for _, t := range tasks {
		if t.Status == "queued" {
			t.Status = "blocked"
			t.Error = "退出时暂停，未向平台提交；可显式恢复"
			s.store.Put("task", t.ID, t)
		}
	}
}
func NewService(root string) (*Service, error) {
	st, e := Open(root)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{store: st, provider: NewProvider(), ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), allowLocalProduct: qaLocalProduct}
	tasks, e := List[Task](st, "task")
	if e != nil {
		st.Close()
		cancel()
		return nil, e
	}
	for _, t := range tasks {
		if t.Status == "queued" && t.AgentRunID != "" {
			var run AgentRun
			if st.Get("agent_run", t.AgentRunID, &run) == nil && run.AutoGeneration {
				t.Status = "blocked"
				t.Error = "上次制作中断，此图尚未提交，请明确恢复生成"
				if e = st.Put("task", t.ID, t); e != nil {
					st.Close()
					cancel()
					return nil, e
				}
			}
		}
		if t.Status == "submitting" || t.Status == "running" {
			t.Status = "unknown"
			t.Error = "应用上次在提交期间退出，结果未知。请先核对平台记录。"
			if e = st.Put("task", t.ID, t); e != nil {
				st.Close()
				cancel()
				return nil, e
			}
		}
	}
	s.inflight = map[string]bool{}
	s.imageConcurrency = defaultImageConcurrency
	var saved int
	if st.Get("preferences", "image-concurrency", &saved) == nil && saved >= 1 && saved <= maxImageConcurrency {
		s.imageConcurrency = saved
	}
	if e = s.initializeAgents(); e != nil {
		st.Close()
		cancel()
		return nil, e
	}
	_ = st.appendLog("info", "app", "", "应用已启动；本地日志保留最近5000条")
	return s, nil
}
func (s *Service) Start(emit func()) { s.emit = emit; go s.loop() }
func (s *Service) Close() {
	s.cancel()
	s.waitAgent()
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store.Close()
}
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
	if s.emit != nil {
		s.emit()
	}
}
func (s *Service) key(ref string) (string, error) {
	if ref == "" {
		return "", errors.New("请在设置中保存 API Key")
	}
	b, e := readOwned(s.store.Root, "secrets/"+ref+".bin")
	if e != nil {
		return "", errors.New("密钥不可用，请在设置中重新输入")
	}
	plain, e := Unprotect(b)
	if e != nil {
		return "", errors.New("密钥无法在当前 Windows 用户下解密，请重新输入")
	}
	return string(plain), nil
}
func cleanConnection(c Connection) Connection {
	c = normalizeConnection(c)
	c.HasKey = c.Credential != ""
	c.Credential = ""
	return c
}
func (s *Service) SaveConnection(in ConnectionInput) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	base, e := NormalizeURL(in.BaseURL)
	if e != nil {
		return Connection{}, e
	}
	if strings.TrimSpace(in.Name) == "" {
		return Connection{}, errors.New("请填写连接名称")
	}
	var c Connection
	if in.ID != "" {
		if e = s.store.Get("connection", in.ID, &c); e != nil {
			return c, e
		}
	} else {
		c.ID = NewID()
	}
	c.Name = strings.TrimSpace(in.Name)
	kind := in.Kind
	if kind == "" {
		kind = "image"
	}
	if kind != "image" && kind != "llm" {
		return c, errors.New("连接用途无效")
	}
	if in.ID != "" && normalizeConnection(c).Kind != kind {
		return c, errors.New("不能修改连接用途，请另建连接")
	}
	if c.BaseURL != base || c.Model != strings.TrimSpace(in.Model) || in.Key != "" || in.ClearKey || c.Mode != in.Mode || c.AuthMode != in.AuthMode {
		c.Capabilities = LLMCapabilities{}
	}
	c.Kind = kind
	c.BaseURL = base
	c.Model = strings.TrimSpace(in.Model)
	c.Protocol = "openai-images"
	if kind == "llm" {
		maximum := in.MaxOutputTokens
		if maximum == 0 {
			maximum = 8192
		}
		if maximum < 256 || maximum > 65536 {
			return c, errors.New("LLM 输出上限应为 256–65536 tokens")
		}
		if c.MaxOutputTokens != maximum {
			c.Capabilities = LLMCapabilities{}
		}
		c.MaxOutputTokens = maximum
		c.Protocol = "anthropic-messages"
		c.Mode = in.Mode
		if c.Mode == "" {
			c.Mode = "claude"
		}
		c.AuthMode = in.AuthMode
		if c.AuthMode == "" {
			c.AuthMode = "api-key"
		}
		if c.Mode != "claude" && c.Mode != "compatible" {
			return c, errors.New("LLM 兼容模式无效")
		}
		if c.AuthMode != "api-key" && c.AuthMode != "bearer" {
			return c, errors.New("认证方式无效")
		}
	}
	if in.ClearKey {
		c.Credential = ""
	}
	if in.Key != "" {
		if strings.ContainsAny(in.Key, "\r\n") {
			return c, errors.New("密钥不能包含换行")
		}
		cipher, e := Protect([]byte(strings.TrimSpace(in.Key)))
		if e != nil {
			return c, e
		}
		ref := NewID()
		if e = atomicWrite(filepath.Join(s.store.Root, "secrets", ref+".bin"), cipher); e != nil {
			return c, e
		}
		c.Credential = ref
	}
	if e = s.store.Put("connection", c.ID, c); e != nil {
		return c, e
	}
	return cleanConnection(c), nil
}
func (s *Service) ListConnections() ([]Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs, e := List[Connection](s.store, "connection")
	for i := range cs {
		cs[i] = cleanConnection(cs[i])
	}
	return cs, e
}
func (s *Service) TestConnection(in ConnectionInput) (result TestResult, resultErr error) {
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		level, text := "info", "模型查询："+result.Auth+"；"+result.Models
		if resultErr != nil {
			level, text = "warn", "模型查询："+resultErr.Error()
		}
		if result.Models == "未获取" || (result.Models != "" && !strings.HasPrefix(result.Models, "已获取")) {
			level = "warn"
		}
		_ = s.store.appendLog(level, "connection", in.ID, redact(text, strings.TrimSpace(in.Key)))
	}()
	s.mu.Lock()
	base, e := NormalizeURL(in.BaseURL)
	if e != nil {
		s.mu.Unlock()
		return TestResult{}, e
	}
	key := in.Key
	if key == "" && in.ID != "" {
		var c Connection
		if e = s.store.Get("connection", in.ID, &c); e == nil {
			key, e = s.key(c.Credential)
		}
	}
	s.mu.Unlock()
	if e != nil {
		return TestResult{}, e
	}
	if strings.TrimSpace(key) == "" {
		return TestResult{}, errors.New("请填写 API Key，或先保存连接")
	}
	key = strings.TrimSpace(key)
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	id := in.TestRequestID
	if id == "" {
		id = NewID()
	}
	s.mu.Lock()
	if s.connectionTests == nil {
		s.connectionTests = map[string]context.CancelFunc{}
	}
	if _, exists := s.connectionTests[id]; exists {
		s.mu.Unlock()
		cancel()
		return TestResult{}, errors.New("连接测试正在进行")
	}
	s.connectionTests[id] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.connectionTests, id); s.mu.Unlock() }()
	ids, e := s.provider.Models(ctx, normalizeConnection(Connection{BaseURL: base, Kind: in.Kind, AuthMode: in.AuthMode}), key)
	if errors.Is(ctx.Err(), context.Canceled) {
		return TestResult{}, errors.New("连接测试已取消")
	}
	r := TestResult{Auth: "待验证", Models: "未获取", Generation: "未执行生图验证（不会产生生成费用）", IDs: []string{}}
	if e != nil {
		r.Models = e.Error()
		if strings.Contains(e.Error(), "HTTP 401") || strings.Contains(e.Error(), "HTTP 403") {
			r.Auth = "认证被拒绝，请检查 Key 和权限"
		} else {
			r.Auth = "无法仅凭模型查询确认；可手动填写模型"
		}
		return r, nil
	}
	sort.Strings(ids)
	r.Auth = "模型查询认证通过"
	r.Models = fmt.Sprintf("已获取 %d 个模型；生图权限仍需实际请求验证", len(ids))
	r.IDs = ids
	if in.Kind == "llm" {
		r.Models = fmt.Sprintf("已获取 %d 个模型；Agent 能力需另外验证", len(ids))
		r.Generation = "仅查询模型，未调用 LLM"
	}
	return r, nil
}
func (s *Service) CancelConnectionTest(id string) {
	s.mu.Lock()
	cancel := s.connectionTests[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (s *Service) DeleteConnection(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentRunID != "" {
		var run AgentRun
		if s.store.Get("agent_run", s.agentRunID, &run) == nil && (run.Input.LLMConnectionID == id || run.Input.ImageConnectionID == id) {
			return errors.New("智能生图正在使用此连接，请先取消分析")
		}
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return e
	}
	for _, t := range tasks {
		if t.Connection.ID == id && active(t.Status) {
			return errors.New("此连接有待执行或活动任务，请先停止待执行项并等待活动任务结束")
		}
	}
	return s.store.Delete("connection", id)
}
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
	result, e := s.store.Submit(in.RequestID, batch, tasks)
	if e == nil {
		s.signal()
	}
	return result, e
}
func (s *Service) ListTasks(status string, offset, limit int) (Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := List[Task](s.store, "task")
	if e != nil {
		return Page{}, e
	}
	filtered := []Task{}
	for _, t := range all {
		if status == "" || t.Status == status {
			t.Connection = cleanConnection(t.Connection)
			t.Results = nil
			filtered = append(filtered, t)
		}
	}
	a, b := bounds(offset, limit, len(filtered))
	return Page{filtered[a:b], len(filtered)}, nil
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
		var c Connection
		if e := s.store.Get("connection", t.Connection.ID, &c); e != nil {
			return "", e
		}
		if normalizeConnection(c).Kind != "image" {
			return "", errors.New("请选择生图中转站")
		}
		if _, e := s.key(c.Credential); e != nil {
			return "", e
		}
		t.ID = NewID()
		t.CreatedAt = Now()
		t.UpdatedAt = Now()
		t.Status = "queued"
		t.Error = ""
		t.Results = nil
		t.AssetIDs = []string{}
		t.Diagnostic = GenerationDiagnostic{}
		t.Connection = c
		batch, e := s.store.Submit(NewID(), t.BatchID, []Task{t})
		if e == nil {
			s.signal()
		}
		return batch, e
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
	if t.Status != "blocked" {
		return errors.New("只有未提交且缺少密钥的任务可恢复")
	}
	var c Connection
	if e := s.store.Get("connection", t.Connection.ID, &c); e != nil {
		return e
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
	if e := s.store.Put("task", id, t); e != nil {
		return e
	}
	s.signal()
	return nil
}
func (s *Service) Dashboard() (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts, e := List[Task](s.store, "task")
	if e != nil {
		return Summary{}, e
	}
	as, e := List[Asset](s.store, "asset")
	r := Summary{Tasks: len(ts), DataDir: s.store.Root, Version: "0.3.8"}
	if s.agentRunID != "" {
		r.Active++
	}
	for _, t := range ts {
		if active(t.Status) {
			r.Active++
		}
	}
	for _, a := range as {
		if !a.Hidden {
			r.Assets++
		}
	}
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

// loop dispatches queued tasks oldest first, keeping up to the configured number
// in flight. Each task is still submitted once; parallelism never adds retries.
func (s *Service) loop() {
	defer close(s.done)
	// Close waits for in-flight requests before the store is closed.
	defer s.workers.Wait()
	for {
		if s.ctx.Err() != nil {
			return
		}
		started := false
		for s.claimTask() {
			started = true
		}
		if !started {
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
			case <-time.After(time.Second):
			}
		}
	}
}

// claimTask starts one waiting task in its own goroutine when a slot is free.
func (s *Service) claimTask() bool {
	s.mu.Lock()
	if s.paused || s.ctx.Err() != nil || len(s.inflight) >= s.imageConcurrency {
		s.mu.Unlock()
		return false
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		s.mu.Unlock()
		return false
	}
	var chosen *Task
	for i := len(tasks) - 1; i >= 0; i-- {
		if (tasks[i].Status == "queued" || tasks[i].Status == "downloading") && !s.inflight[tasks[i].ID] {
			t := tasks[i]
			chosen = &t
			break
		}
	}
	if chosen == nil {
		s.mu.Unlock()
		return false
	}
	s.inflight[chosen.ID] = true
	s.workers.Add(1)
	s.mu.Unlock()
	go func(t Task) {
		defer func() {
			s.mu.Lock()
			delete(s.inflight, t.ID)
			s.mu.Unlock()
			s.workers.Done()
			s.signal()
		}()
		s.step(t)
	}(*chosen)
	return true
}

// step runs one claimed task: submit once, then download its result.
func (s *Service) step(claimed Task) bool {
	s.mu.Lock()
	// Re-read under the lock: the task may have been stopped since it was claimed.
	var t Task
	if s.store.Get("task", claimed.ID, &t) != nil || (t.Status != "queued" && t.Status != "downloading") || s.paused {
		s.mu.Unlock()
		return false
	}
	if t.Status == "downloading" {
		s.mu.Unlock()
		s.download(&t)
		return true
	}
	key, e := s.key(t.Connection.Credential)
	if e != nil {
		t.Status = "blocked"
		t.Error = e.Error()
		s.store.Put("task", t.ID, t)
		s.mu.Unlock()
		s.signal()
		return true
	}
	refs := []Asset{}
	for _, id := range t.References {
		var a Asset
		if e = s.store.Get("asset", id, &a); e != nil {
			break
		}
		refs = append(refs, a)
	}
	if e != nil {
		t.Status = "failed"
		t.Error = "参考图记录不可用"
		s.store.Put("task", t.ID, t)
		s.mu.Unlock()
		s.signal()
		return true
	}
	t.Status = "submitting"
	t.UpdatedAt = Now()
	if e = s.store.Put("task", t.ID, t); e != nil {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	s.signal()
	outputs, unknown, e := s.provider.GenerateWithProgress(s.ctx, t, key, refs, s.store.Root, func(d GenerationDiagnostic) {
		s.mu.Lock()
		t.Diagnostic = d
		s.store.Put("task", t.ID, t)
		s.mu.Unlock()
		s.signal()
	})
	s.mu.Lock()
	if e != nil {
		t.Status = "failed"
		if unknown {
			t.Status = "unknown"
		}
		t.Error = redact(e.Error(), key)
	} else {
		t.Status = "downloading"
		t.Results = outputs
		t.Error = ""
	}
	t.UpdatedAt = Now()
	saveErr := s.store.Put("task", t.ID, t)
	s.mu.Unlock()
	s.signal()
	if saveErr != nil {
		return false
	}
	if e == nil {
		s.download(&t)
	}
	return true
}
func (s *Service) download(t *Task) {
	for i, out := range t.Results {
		assetID := fmt.Sprintf("%s-%d", t.ID, i)
		s.mu.Lock()
		var existing Asset
		already := s.store.Get("asset", assetID, &existing) == nil
		s.mu.Unlock()
		if !already {
			b, e := s.outputBytes(s.ctx, out)
			if e != nil {
				s.downloadError(t, e)
				return
			}
			s.mu.Lock()
			_, e = s.saveImage(b, assetID+".png", "generated", t.ID, t.ProjectID, assetID)
			s.mu.Unlock()
			if e != nil {
				s.downloadError(t, e)
				return
			}
		}
		found := false
		for _, id := range t.AssetIDs {
			if id == assetID {
				found = true
			}
		}
		if !found {
			t.AssetIDs = append(t.AssetIDs, assetID)
		}
	}
	s.mu.Lock()
	t.Status = "succeeded"
	t.Error = ""
	t.Results = nil
	t.UpdatedAt = Now()
	s.store.Put("task", t.ID, *t)
	s.mu.Unlock()
	s.signal()
}
func (s *Service) downloadError(t *Task, e error) {
	s.mu.Lock()
	t.Status = "download_failed"
	t.Error = e.Error()
	t.UpdatedAt = Now()
	s.store.Put("task", t.ID, *t)
	s.mu.Unlock()
	s.signal()
}
func (s *Service) ListProjects() ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return List[Project](s.store, "project")
}
func (s *Service) SaveProject(p Project) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return p, errors.New("请填写项目名称")
	}
	if p.ID == "" {
		p.ID = NewID()
		p.CreatedAt = Now()
	}
	return p, s.store.Put("project", p.ID, p)
}
func (s *Service) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	assets, e := List[Asset](s.store, "asset")
	if e != nil {
		return e
	}
	for _, a := range assets {
		if a.ProjectID == id {
			a.ProjectID = ""
			if e = s.store.Put("asset", a.ID, a); e != nil {
				return e
			}
		}
	}
	return s.store.Delete("project", id)
}
func (s *Service) ListTemplates() ([]Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return List[Template](s.store, "template")
}
func (s *Service) SaveTemplate(t Template) (Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Prompt) == "" {
		return t, errors.New("请填写模板名称与提示词")
	}
	if t.ID == "" {
		t.ID = NewID()
	}
	return t, s.store.Put("template", t.ID, t)
}
func (s *Service) DeleteTemplate(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.Delete("template", id)
}
func (s *Service) Root() string { s.mu.Lock(); defer s.mu.Unlock(); return s.store.Root }
