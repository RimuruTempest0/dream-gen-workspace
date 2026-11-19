package core

// Image and LLM connections: save, list, test and delete.

import (
	"context"
	"errors"
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
		return Connection{}, errors.New(strings.TrimSpace(s.text(s.lic.message)))
	}
	// The hosted edition of the mass-market line runs on the provider's lines only.
	if s.hostedLocked() {
		return Connection{}, errors.New(s.text(hostedLinesOnly))
	}
	return s.saveConnectionLocked(in)
}
func (s *Service) saveConnectionLocked(in ConnectionInput) (Connection, error) {
	base, e := NormalizeURL(in.BaseURL, s.Language())
	if e != nil {
		return Connection{}, e
	}
	if strings.TrimSpace(in.Name) == "" {
		return Connection{}, s.fail("connection.nameRequired")
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
		return c, s.fail("connection.kindInvalid")
	}
	if in.ID != "" && normalizeConnection(c).Kind != kind {
		return c, s.fail("connection.kindLocked")
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
			return c, s.fail("connection.protocolInvalid")
		}
		c.Protocol = in.Protocol
	}
	if kind == "llm" {
		maximum := LLMOutputLimit(in.MaxOutputTokens)
		if maximum < 256 || maximum > 65536 {
			return c, s.fail("connection.llm.outputLimitRange")
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
			return c, s.fail("connection.llm.modeInvalid")
		}
		if c.AuthMode != "api-key" && c.AuthMode != "bearer" {
			return c, s.fail("connection.authInvalid")
		}
	}
	if in.ClearKey {
		c.Credential = ""
	}
	if in.Key != "" {
		if strings.ContainsAny(in.Key, "\r\n") {
			return c, s.fail("connection.key.newline")
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
		// In the hosted edition the user's own services are kept but out of use.
		if s.hostedLocked() && c.Managed == "" {
			continue
		}
		cs = append(cs, cleanConnection(c))
	}
	return cs, e
}
func (s *Service) TestConnection(in ConnectionInput) (result TestResult, resultErr error) {
	listed := false // the model list was read
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		level, text := "info", s.msg("connection.test.log", "auth", result.Auth, "models", result.Models)
		if resultErr != nil {
			level, text = "warn", s.msg("connection.test.logError", "error", resultErr.Error())
		}
		if result.Models != "" && !listed {
			level = "warn"
		}
		_ = s.store.appendLog(level, "connection", in.ID, s.redact(text, strings.TrimSpace(in.Key)))
	}()
	s.mu.Lock()
	base, e := NormalizeURL(in.BaseURL, s.Language())
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
		return TestResult{}, s.fail("connection.test.needsKey")
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
		return TestResult{}, s.fail("connection.test.running")
	}
	s.connectionTests[id] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.connectionTests, id); s.mu.Unlock() }()
	ids, e := s.provider.Models(s.langCtx(ctx), normalizeConnection(Connection{BaseURL: base, Kind: in.Kind, AuthMode: in.AuthMode}), key)
	if errors.Is(ctx.Err(), context.Canceled) {
		return TestResult{}, s.fail("connection.test.canceled")
	}
	r := TestResult{Auth: s.msg("connection.test.authPending"), Models: s.msg("connection.test.modelsNone"), Generation: s.msg("connection.test.noGeneration"), IDs: []string{}}
	if e != nil {
		r.Models = e.Error()
		if strings.Contains(e.Error(), "HTTP 401") || strings.Contains(e.Error(), "HTTP 403") {
			r.Auth = s.msg("connection.test.authRejected")
		} else {
			r.Auth = s.msg("connection.test.authUnknown")
		}
		return r, nil
	}
	sort.Strings(ids)
	listed = true
	r.Auth = s.msg("connection.test.authOK")
	r.Models = s.msg("connection.test.modelsImage", "n", len(ids))
	r.IDs = ids
	if in.ID != "" && strings.TrimSpace(in.Key) == "" {
		s.saveModels(in.ID, base, ids)
	}
	if in.Kind == "llm" {
		r.Models = s.msg("connection.test.modelsLLM", "n", len(ids))
		r.Generation = s.msg("connection.test.llmNotCalled")
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
			return s.fail("connection.inUseByCreation")
		}
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return e
	}
	for _, t := range tasks {
		if t.Connection.ID == id && active(t.Status) {
			return s.fail("connection.hasActiveTasks")
		}
	}
	if e = s.store.Delete("connection", id); e != nil {
		return e
	}
	_ = s.store.Delete(modelListKind, id)
	s.removeGatewayLocked(id)
	return nil
}
