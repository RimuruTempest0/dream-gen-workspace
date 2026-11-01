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
	downloads        map[string]bool
	// genGateway maps an in-flight generation to its gateway (per-gateway limits).
	genGateway map[string]string
	// genCancel stops the request of an in-flight generation; canceled marks the
	// ones the user cancelled, so the result is recorded as cancelled.
	genCancel map[string]context.CancelFunc
	canceled  map[string]bool
	// probes holds the latest free health check (model list) of each image relay.
	probes  map[string]probeResult
	probing map[string]bool
	// lic is the customer licence (empty in developer mode).
	lic licenseState
	// llmCallsSaved counts LLM call records written; old ones are pruned now and then.
	llmCallsSaved int
	workers       sync.WaitGroup
}

const (
	defaultImageConcurrency = 3
	maxImageConcurrency     = 4
	// Result downloads never call the generation API, so they get their own limit.
	maxParallelDownloads = 4
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
	s.downloads = map[string]bool{}
	s.genGateway = map[string]string{}
	s.genCancel = map[string]context.CancelFunc{}
	s.canceled = map[string]bool{}
	s.probes = map[string]probeResult{}
	s.probing = map[string]bool{}
	s.imageConcurrency = defaultImageConcurrency
	var saved int
	if st.Get("preferences", "image-concurrency", &saved) == nil && saved >= 1 {
		// Versions before 0.5.0 allowed up to 8; keep such a choice at the new maximum.
		s.imageConcurrency = min(saved, maxImageConcurrency)
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
	if e := s.refuseManagedLocked(in.ID); e != nil {
		return Connection{}, e
	}
	return s.saveConnectionLocked(in)
}
func (s *Service) saveConnectionLocked(in ConnectionInput) (Connection, error) {
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
		maximum := LLMOutputLimit(in.MaxOutputTokens)
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
	all, e := List[Connection](s.store, "connection")
	cs := []Connection{}
	for _, c := range all {
		// During the trial the customer sees only the main lines.
		if s.lic.mode == "trial" && !mainLine(c) {
			continue
		}
		cs = append(cs, cleanConnection(c))
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
	if in.ID != "" && strings.TrimSpace(in.Key) == "" {
		s.saveModels(in.ID, base, ids)
	}
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
	if e := s.refuseManagedLocked(id); e != nil {
		return e
	}
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
	if e = s.store.Delete("connection", id); e != nil {
		return e
	}
	_ = s.store.Delete(modelListKind, id)
	s.removeGatewayLocked(id)
	return nil
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
	ts, e := List[Task](s.store, "task")
	if e != nil {
		return Summary{}, e
	}
	as, e := List[Asset](s.store, "asset")
	r := Summary{Tasks: len(ts), DataDir: s.store.Root, Version: "0.6.20"}
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
		for {
			downloading := s.claimTask(true)
			generating := s.claimTask(false)
			if !downloading && !generating {
				break
			}
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

// claimTask starts one waiting task in its own goroutine when its pool has room.
// Generation requests are limited by the user's setting; downloads of results
// already returned use a separate pool, so a slow image host never holds a
// generation slot.
func (s *Service) claimTask(download bool) bool {
	s.mu.Lock()
	pool, limit, status := s.inflight, s.imageConcurrency, "queued"
	if download {
		pool, limit, status = s.downloads, maxParallelDownloads, "downloading"
	}
	if s.paused || s.ctx.Err() != nil || len(pool) >= limit {
		s.mu.Unlock()
		return false
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		s.mu.Unlock()
		return false
	}
	var chosen *Task
	var hc *healthCache
	var g GatewaySettings
	if !download {
		s.maybeProbeLocked(tasks)
		hc = &healthCache{s: s, tasks: tasks, now: time.Now(), seen: map[string]GatewayHealth{}}
		g = s.gatewaySettingsLocked()
	}
	for i := len(tasks) - 1; i >= 0; i-- {
		if tasks[i].Status == status && !s.inflight[tasks[i].ID] && !s.downloads[tasks[i].ID] {
			t := tasks[i]
			// An image drawn from the suite's detail sample waits until the sample
			// is done; nothing is sent (or billed) while it waits.
			if !download && t.SamplePending {
				if !attachSample(&t, tasks) {
					continue
				}
				_ = s.store.Put("task", t.ID, t)
			}
			// Past the trial limit (or with an invalid licence) nothing more is sent.
			if !download && s.trialStopLocked(tasks) {
				t.Status, t.UpdatedAt = "blocked", Now()
				t.Error = "此图尚未提交、未计费。" + trialUsedUp
				if msg := s.licenseBlockMessageLocked(); msg != "" {
					t.Error = "此图尚未提交、未计费。" + msg
				}
				_ = s.store.Put("task", t.ID, t)
				continue
			}
			// An image is sent only to a gateway that is not known to be failing;
			// it may move to a healthy backup first (it was never sent, no cost).
			if !download && !s.routeLocked(&t, g, hc) {
				continue
			}
			chosen = &t
			break
		}
	}
	if chosen == nil {
		s.mu.Unlock()
		return false
	}
	pool[chosen.ID] = true
	if !download {
		s.genGateway[chosen.ID] = chosen.Connection.ID
	}
	s.workers.Add(1)
	s.mu.Unlock()
	go func(t Task) {
		defer func() {
			s.mu.Lock()
			delete(pool, t.ID)
			delete(s.genGateway, t.ID)
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
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	s.genCancel[t.ID] = cancel
	s.mu.Unlock()
	s.signal()
	outputs, unknown, e := s.provider.GenerateWithProgress(ctx, t, key, refs, s.store.Root, func(d GenerationDiagnostic) {
		s.mu.Lock()
		t.Diagnostic = d
		s.store.Put("task", t.ID, t)
		s.mu.Unlock()
		s.signal()
	})
	s.mu.Lock()
	delete(s.genCancel, t.ID)
	userCanceled := s.canceled[t.ID]
	delete(s.canceled, t.ID)
	// A result that arrived anyway is kept (it may be billed); otherwise the
	// cancellation is recorded instead of a failure.
	if userCanceled && e != nil {
		t.Status, t.UpdatedAt = "canceled", Now()
		t.Error = canceledInFlight
		saveErr := s.store.Put("task", t.ID, t)
		_ = s.store.appendLog("warn", "image", t.ID, fmt.Sprintf("已取消生成：请求发出后等待 %d 秒时取消", t.Diagnostic.ElapsedMS/1000))
		s.mu.Unlock()
		s.signal()
		return saveErr == nil
	}
	// A rejected key or an empty account fails every remaining image the same way.
	// With a gateway setup, the gateway is marked down and unsent images move to a
	// backup or pause there; without one, the rest of the batch pauses unsent.
	rejected := ""
	if e != nil && !unknown {
		rejected = rejectionKind(t.Diagnostic.HTTPStatus, e.Error())
	}
	if e != nil {
		t.Status = "failed"
		if unknown {
			t.Status = "unknown"
		}
		t.Error = redact(e.Error(), key)
		switch {
		case rejected == "balance" && t.Trial:
			// The trial key's upstream quota is the hard trial limit.
			t.Error = fmt.Sprintf("试用额度已用完（HTTP %d，中转站返回额度不足），此图未生成也不计费。请在“连接与设置”输入激活码后继续使用。原始错误：%s", t.Diagnostic.HTTPStatus, t.Error)
		case rejected == "balance":
			t.Error = fmt.Sprintf("中转站账户余额不足（HTTP %d），中转站拒绝了请求，未生成也不计费。请到该中转站后台充值后重新生成，API Key 不需要重新输入。原始错误：%s", t.Diagnostic.HTTPStatus, t.Error)
		case rejected == "auth":
			t.Error = fmt.Sprintf("API Key 无效（HTTP %d），生图中转站拒绝了请求，未生成也不计费。请在“连接与设置”重新输入该生图中转站的 API Key 后重新生成。原始错误：%s", t.Diagnostic.HTTPStatus, t.Error)
			// Re-check the key right away with the free probe.
			delete(s.probes, t.Connection.ID)
		}
		// A trial key at its quota ends the trial: pause the rest, whatever the routing.
		if rejected != "" && (!s.gatewaySetupLocked() || t.Trial) {
			kind := rejected
			if kind == "balance" && t.Trial {
				kind = "trial"
			}
			s.pauseBatchForRejectionLocked(t, kind)
		}
		if unknown && relayTimedOut(t.Diagnostic) {
			t.Error = fmt.Sprintf("中转站等待约 %d 秒后断开（%s），没有返回确定的生图结果。这张图可能仍在中转站生成并已计费，请先核对中转站记录。降低同时生成张数或质量可能减少这类超时，但不能保证避免。%s原始错误：%s", t.Diagnostic.ElapsedMS/1000, timeoutLabel(t.Diagnostic), s.autoRegenerateLocked(&t), t.Error)
		}
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
	// The result is saved as "downloading"; the download pool fetches it, so this
	// generation slot is free for the next image right away.
	return true
}

// relayTimedOut recognises a relay or CDN cutting a long-running request: gateway
// timeout statuses, or a connection closed without a reply after a long wait.
func relayTimedOut(d GenerationDiagnostic) bool {
	switch d.HTTPStatus {
	case 504, 522, 524:
		return true
	case 0:
		return d.ElapsedMS >= 90_000 && d.NetworkError != ""
	}
	return false
}

func timeoutLabel(d GenerationDiagnostic) string {
	if d.HTTPStatus != 0 {
		return fmt.Sprintf("HTTP %d", d.HTTPStatus)
	}
	return "连接被关闭"
}

// pauseBatchForRejectionLocked blocks the batch's queued, unsent images after the
// relay rejected the API key or the account has no balance. Requests already in
// flight finish on their own.
func (s *Service) pauseBatchForRejectionLocked(failed Task, kind string) {
	reason, fix, logText := "API Key 无效", "重新输入密钥后可恢复生成。", "生图中转站拒绝 API Key"
	if kind == "balance" {
		reason, fix, logText = "中转站账户余额不足", "到中转站充值后可恢复生成。", "生图中转站账户余额不足"
	} else if kind == "trial" {
		reason, fix, logText = "试用额度已用完", "输入激活码后可恢复生成。", "试用额度已用完"
	}
	if failed.BatchID == "" {
		return
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return
	}
	paused := 0
	for _, t := range tasks {
		if t.BatchID == failed.BatchID && t.Status == "queued" && t.Connection.ID == failed.Connection.ID {
			t.Status = "blocked"
			t.Error = "同批图片因" + reason + "已暂停，尚未提交、未计费。" + fix
			t.UpdatedAt = Now()
			if s.store.Put("task", t.ID, t) == nil {
				paused++
			}
		}
	}
	if paused > 0 {
		_ = s.store.appendLog("warn", "image", failed.BatchID, fmt.Sprintf("%s，已暂停同批 %d 张未提交图片", logText, paused))
	}
}

func (s *Service) download(t *Task) {
	s.mu.Lock()
	t.Diagnostic.DownloadStartedAt = Now()
	t.Diagnostic.DownloadMS = 0
	s.store.Put("task", t.ID, *t)
	s.mu.Unlock()
	s.signal()
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
	t.Diagnostic.DownloadMS = sinceMS(t.Diagnostic.DownloadStartedAt)
	t.UpdatedAt = Now()
	s.store.Put("task", t.ID, *t)
	s.mu.Unlock()
	s.signal()
}

// sinceMS returns milliseconds elapsed since an RFC 3339 timestamp, or 0.
func sinceMS(stamp string) int64 {
	at, e := time.Parse(time.RFC3339Nano, stamp)
	if e != nil {
		return 0
	}
	return time.Since(at).Milliseconds()
}

func (s *Service) downloadError(t *Task, e error) {
	s.mu.Lock()
	t.Status = "download_failed"
	t.Error = e.Error()
	t.Diagnostic.DownloadMS = sinceMS(t.Diagnostic.DownloadStartedAt)
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
