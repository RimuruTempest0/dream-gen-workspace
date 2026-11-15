package core

// Image and LLM connections: save, list, test and delete.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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
	// A customer build whose licence is missing or invalid cannot be turned into a
	// free build by adding its own lines.
	if s.lic.mode == "invalid" || s.lic.mode == "mismatch" {
		return Connection{}, errors.New(strings.TrimSpace(s.lic.message))
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
	c.Protocol = protocolOpenAIImages
	if kind == "image" && in.Protocol != "" {
		if _, ok := imageProtocols[in.Protocol]; !ok {
			return c, errors.New("图片协议无效")
		}
		c.Protocol = in.Protocol
	}
	if kind == "llm" {
		maximum := LLMOutputLimit(in.MaxOutputTokens)
		if maximum < 256 || maximum > 65536 {
			return c, errors.New("AI 模型输出上限应为 256–65536 tokens")
		}
		// Raising an old default (or keeping it) is not a change that needs a new check.
		if LLMOutputLimit(c.MaxOutputTokens) != maximum {
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
			return c, errors.New("AI 模型兼容模式无效")
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
		r.Generation = "仅查询模型，未调用 AI 模型"
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
			return errors.New("创作正在使用此连接，请先取消分析")
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
