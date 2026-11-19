package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"imageworkbench/internal/i18n"
)

// DefaultMaxOutputTokens is the LLM single-reply output limit when none is set.
// The limit covers the model's thinking too. Earlier defaults are raised when
// stored: 8192 (before 0.6.10) cut long set plans short, and 32768 (0.6.10–0.7.1)
// was spent on thinking alone in some runs. At about 90 tokens a second, 65536
// still fits within one run's 20-minute limit.
const DefaultMaxOutputTokens = 65536

var legacyDefaultMaxOutputTokens = []int{8192, 32768}

// LLMOutputLimit returns the limit to use for a stored value (0 or an old default means the default).
func LLMOutputLimit(n int) int {
	if n == 0 || slices.Contains(legacyDefaultMaxOutputTokens, n) {
		return DefaultMaxOutputTokens
	}
	return n
}

func normalizeConnection(c Connection) Connection {
	if c.Kind == "" {
		c.Kind = "image"
	}
	if c.Kind == "llm" {
		c.MaxOutputTokens = LLMOutputLimit(c.MaxOutputTokens)
		if c.Mode == "" {
			c.Mode = "claude"
		}
		if c.AuthMode == "" {
			c.AuthMode = "api-key"
		}
	}
	return c
}
func setProviderAuth(r *http.Request, c Connection, key string) {
	if c.Kind == "llm" && c.AuthMode != "bearer" {
		r.Header.Set("x-api-key", key)
	} else {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	if c.Kind == "llm" {
		r.Header.Set("anthropic-version", "2023-06-01")
	}
}

// Optional Claude extensions are removed only in the explicitly selected compatibility mode.
func basicMessages(v any) {
	// Request options are top-level fields. A thinking content block belongs to
	// conversation history and must survive tool-result round trips unchanged.
	if root, ok := v.(map[string]any); ok {
		for _, k := range []string{"thinking", "output_config", "context_management"} {
			delete(root, k)
		}
	}
	stripMessageExtensions(v)
}
func stripMessageExtensions(v any) {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"cache_control", "defer_loading", "eager_input_streaming", "strict"} {
			delete(x, k)
		}
		for _, child := range x {
			stripMessageExtensions(child)
		}
	case []any:
		for _, child := range x {
			stripMessageExtensions(child)
		}
	}
}
func llmHTTPClient() *http.Client {
	transport := newNetworkTransport()
	// The default 10 seconds is short through a local proxy: a passing slowdown failed
	// several requests in a row while the provider itself was fine.
	transport.TLSHandshakeTimeout = 30 * time.Second
	return &http.Client{Transport: transport, Timeout: 10 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
}
func messagesRequest(ctx context.Context, client *http.Client, c Connection, key string, body any) (*http.Response, error) {
	b, e := json.Marshal(body)
	if e != nil {
		return nil, e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/messages", bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	r.Header.Set("Content-Type", "application/json")
	setProviderAuth(r, c, key)
	return client.Do(r)
}

type messageBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}
type messageResponse struct {
	Content    []messageBlock `json:"content"`
	Usage      usage          `json:"usage"`
	StopReason string         `json:"stop_reason"`
}

func readMessage(resp *http.Response, key, lang string) (messageResponse, error) {
	defer resp.Body.Close()
	var raw json.RawMessage
	e := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw)
	if resp.StatusCode != 200 {
		return messageResponse{}, fmt.Errorf("HTTP %d：%s", resp.StatusCode, apiMessage(raw, key, lang))
	}
	if e != nil {
		return messageResponse{}, errors.New(i18n.T(lang, "connection.llm.incompleteMessage"))
	}
	var v messageResponse
	e = json.Unmarshal(raw, &v)
	return v, e
}

// Read event frames until message_stop; the upstream need not close a keep-alive stream.
func readSSE(ctx context.Context, r io.Reader, frame func([]byte) error) error {
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 65536), 16<<20)
	var lines []string
	consume := func() (bool, error) {
		if len(lines) == 0 {
			return false, nil
		}
		f := strings.Join(lines, "\n") + "\n\n"
		lines = nil
		if e := frame([]byte(f)); e != nil {
			return false, e
		}
		for _, line := range strings.Split(f, "\n") {
			if strings.HasPrefix(line, "data:") {
				var v struct {
					Type string `json:"type"`
				}
				if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &v) == nil && v.Type == "message_stop" {
					return true, nil
				}
			}
		}
		return false, nil
	}
	for scan.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSuffix(scan.Text(), "\r")
		if line == "" {
			done, e := consume()
			if e != nil {
				return e
			}
			if done {
				return nil
			}
		} else {
			lines = append(lines, line)
		}
	}
	if done, e := consume(); done || e != nil {
		return e
	}
	if scan.Err() != nil {
		return scan.Err()
	}
	return failCtx(ctx, "connection.llm.streamCut")
}

func (s *Service) ValidateLLMCapabilities(id, testID string) (result LLMCapabilities, resultErr error) {
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		level, text := "info", s.msg("connection.llm.log.checked", "details", strings.Join(result.Details, s.msg("connection.llm.detailSeparator")))
		if resultErr != nil {
			level, text = "warn", s.msg("connection.llm.log.checked", "details", resultErr.Error())
		} else if !result.Text || !result.Vision || !result.Tools || !result.Streaming {
			level = "warn"
		}
		_ = s.store.appendLog(level, "connection", id, text)
	}()
	s.mu.Lock()
	var c Connection
	e := s.store.Get("connection", id, &c)
	c = normalizeConnection(c)
	var key string
	if e == nil && c.Kind != "llm" {
		e = s.fail("connection.llm.chooseLLM")
	}
	if e == nil && c.Model == "" {
		e = s.fail("connection.llm.saveModelFirst")
	}
	if e == nil {
		key, e = s.key(c.Credential)
	}
	if e != nil {
		s.mu.Unlock()
		return LLMCapabilities{}, e
	}
	if testID == "" {
		testID = NewID()
	}
	if s.connectionTests == nil {
		s.connectionTests = map[string]context.CancelFunc{}
	}
	if _, exists := s.connectionTests[testID]; exists {
		s.mu.Unlock()
		return LLMCapabilities{}, s.fail("connection.llm.checkRunning")
	}
	ctx, cancel := context.WithTimeout(s.langCtx(s.ctx), 2*time.Minute)
	s.connectionTests[testID] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.connectionTests, testID); s.mu.Unlock() }()
	cap := LLMCapabilities{Model: c.Model, CheckedAt: Now(), Details: []string{}}
	client := llmHTTPClient()
	probe := func(label string, content any, tools any) (messageResponse, error) {
		body := map[string]any{"model": c.Model, "max_tokens": 128, "messages": []any{map[string]any{"role": "user", "content": content}}}
		if tools != nil {
			body["tools"] = tools
			// Thinking models reject forced tool selection. Match the Agent's
			// normal auto selection and verify the actual tool name and nonce below.
			body["tool_choice"] = map[string]any{"type": "auto"}
			// Include room for reasoning before the tool call, without exceeding
			// the output limit configured for this provider.
			body["max_tokens"] = min(c.MaxOutputTokens, 2048)
		}
		call := s.startLLMCall("test", "", c, nil)
		call.Stage = s.msg("connection.llm.stage", "check", label)
		started := time.Now()
		resp, e := messagesRequest(ctx, client, c, key, body)
		if e == nil {
			call.HTTPStatus = resp.StatusCode
			call.RequestID = resp.Header.Get("request-id")
			v, err := readMessage(resp, key, s.Language())
			if err == nil {
				call.Status, call.FirstByteMS = "completed", max(time.Since(started).Milliseconds(), 1)
				call.addUsage(v.Usage)
				s.saveLLMCall(call, started)
				return v, nil
			}
			e = err
		}
		call.Status, call.Error = "error", redact(e.Error(), key)
		s.saveLLMCall(call, started)
		cap.Details = append(cap.Details, s.msg("connection.llm.detail", "check", label, "result", redact(e.Error(), key)))
		return messageResponse{}, e
	}
	if v, err := probe(s.msg("connection.llm.check.text"), "Reply exactly OK.", nil); err == nil {
		for _, b := range v.Content {
			if b.Type == "text" && strings.TrimSpace(b.Text) == "OK" {
				cap.Text = true
			}
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	expected := "RED"
	shade := color.NRGBA{R: 255, A: 255}
	if NewID()[0] > '7' {
		expected = "BLUE"
		shade = color.NRGBA{B: 255, A: 255}
	}
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			img.SetNRGBA(x, y, shade)
		}
	}
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, img)
	vision := []any{map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": base64.StdEncoding.EncodeToString(pngBuf.Bytes())}}, map[string]any{"type": "text", "text": "Identify the solid color in this image. Reply exactly RED or BLUE."}}
	if ctx.Err() == nil {
		if v, err := probe(s.msg("connection.llm.check.vision"), vision, nil); err == nil {
			for _, b := range v.Content {
				if b.Type == "text" && strings.TrimSpace(b.Text) == expected {
					cap.Vision = true
				}
			}
		}
	}
	nonce := NewID()
	tools := []any{map[string]any{"name": "capability_echo", "description": "Return the provided nonce.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"nonce": map[string]any{"type": "string"}}, "required": []string{"nonce"}}}}
	if ctx.Err() == nil {
		if v, err := probe(s.msg("connection.llm.check.tools"), "Call capability_echo with nonce "+nonce, tools); err == nil {
			for _, b := range v.Content {
				if b.Type == "tool_use" && b.Name == "capability_echo" && b.Input["nonce"] == nonce {
					cap.Tools = true
				}
			}
		}
	}
	if ctx.Err() == nil {
		body := map[string]any{"model": c.Model, "max_tokens": 64, "stream": true, "messages": []any{map[string]any{"role": "user", "content": "Reply exactly OK."}}}
		call := s.startLLMCall("test", "", c, nil)
		call.Stage, call.Stream = s.msg("connection.llm.stage", "check", s.msg("connection.llm.check.streaming")), true
		started := time.Now()
		resp, err := messagesRequest(ctx, client, c, key, body)
		if err == nil {
			call.HTTPStatus = resp.StatusCode
			if resp.StatusCode == 200 && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
				text := ""
				err = readSSE(ctx, resp.Body, func(f []byte) error {
					if call.FirstByteMS == 0 {
						call.FirstByteMS = max(time.Since(started).Milliseconds(), 1)
					}
					call.readStreamUsage(f)
					for _, l := range strings.Split(string(f), "\n") {
						if strings.HasPrefix(l, "data:") {
							var v struct {
								Delta struct {
									Text string `json:"text"`
								} `json:"delta"`
							}
							json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(l, "data:"))), &v)
							text += v.Delta.Text
						}
					}
					return nil
				})
				cap.Streaming = err == nil && strings.TrimSpace(text) == "OK"
			} else {
				_, err = readMessage(resp, key, s.Language())
				if err == nil {
					err = s.fail("connection.llm.noSSE")
				}
			}
			resp.Body.Close()
		}
		call.Status = "completed"
		if err != nil {
			call.Status, call.Error = "error", redact(err.Error(), key)
			cap.Details = append(cap.Details, s.msg("connection.llm.detail", "check", s.msg("connection.llm.check.streaming"), "result", redact(err.Error(), key)))
		}
		s.saveLLMCall(call, started)
	}
	if ctx.Err() != nil {
		return cap, s.fail("connection.llm.checkCanceled")
	}
	for _, check := range []struct {
		label string
		ok    bool
	}{{"connection.llm.check.text", cap.Text}, {"connection.llm.check.vision", cap.Vision}, {"connection.llm.check.tools", cap.Tools}, {"connection.llm.check.streaming", cap.Streaming}} {
		result := s.msg("connection.llm.failed")
		if check.ok {
			result = s.msg("connection.llm.passed")
		}
		cap.Details = append(cap.Details, s.msg("connection.llm.detail", "check", s.msg(check.label), "result", result))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var current Connection
	if e = s.store.Get("connection", id, &current); e != nil {
		return cap, e
	}
	current = normalizeConnection(current)
	if current.BaseURL != c.BaseURL || current.Model != c.Model || current.Credential != c.Credential || current.Mode != c.Mode || current.AuthMode != c.AuthMode || current.MaxOutputTokens != c.MaxOutputTokens {
		return cap, s.fail("connection.llm.changed")
	}
	current.Capabilities = cap
	return cap, s.store.Put("connection", id, current)
}
