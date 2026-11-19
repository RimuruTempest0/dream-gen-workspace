package core

import (
	"context"
	"errors"
	"time"
)

// AskText sends one text prompt to an AI model connection and returns its
// answer. The admin tool uses it with a throw-away service to translate the
// update notes; the call is recorded in the LLM monitor under stage.
func (s *Service) AskText(connectionID, stage, prompt string) (string, error) {
	s.mu.Lock()
	var c Connection
	e := s.store.Get("connection", connectionID, &c)
	c = normalizeConnection(c)
	var key string
	if e == nil && c.Kind != "llm" {
		e = errors.New("请选择 AI 模型")
	}
	if e == nil {
		key, e = s.key(c.Credential)
	}
	s.mu.Unlock()
	if e != nil {
		return "", e
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()
	return s.llmAsk(ctx, c, key, "rewrite", stage, prompt, func(string) error { return nil })
}
