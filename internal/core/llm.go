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
	"strings"
	"time"
)

func normalizeConnection(c Connection) Connection {
	if c.Kind == "" {
		c.Kind = "image"
	}
	if c.Kind == "llm" {
		if c.MaxOutputTokens == 0 {
			c.MaxOutputTokens = 8192
		}
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
	return &http.Client{Transport: newNetworkTransport(), Timeout: 10 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
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
	Content []messageBlock `json:"content"`
}

func readMessage(resp *http.Response, key string) (messageResponse, error) {
	defer resp.Body.Close()
	var raw json.RawMessage
	e := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw)
	if resp.StatusCode != 200 {
		return messageResponse{}, fmt.Errorf("HTTP %d：%s", resp.StatusCode, apiMessage(raw, key))
	}
	if e != nil {
		return messageResponse{}, errors.New("LLM 未返回完整的 Messages JSON 响应")
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
	return errors.New("LLM 流在 message_stop 之前中断")
}

func (s *Service) ValidateLLMCapabilities(id, testID string) (result LLMCapabilities, resultErr error) {
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		level, text := "info", "Agent 能力验证："+strings.Join(result.Details, "；")
		if resultErr != nil {
			level, text = "warn", "Agent 能力验证："+resultErr.Error()
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
		e = errors.New("请选择 LLM 供应商")
	}
	if e == nil && c.Model == "" {
		e = errors.New("请先保存模型 ID")
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
		return LLMCapabilities{}, errors.New("此验证已在进行")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
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
		resp, e := messagesRequest(ctx, client, c, key, body)
		if e == nil {
			v, err := readMessage(resp, key)
			if err == nil {
				return v, nil
			}
			e = err
		}
		cap.Details = append(cap.Details, label+"："+redact(e.Error(), key))
		return messageResponse{}, e
	}
	if v, err := probe("文本", "Reply exactly OK.", nil); err == nil {
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
		if v, err := probe("图片输入", vision, nil); err == nil {
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
		if v, err := probe("工具调用", "Call capability_echo with nonce "+nonce, tools); err == nil {
			for _, b := range v.Content {
				if b.Type == "tool_use" && b.Name == "capability_echo" && b.Input["nonce"] == nonce {
					cap.Tools = true
				}
			}
		}
	}
	if ctx.Err() == nil {
		body := map[string]any{"model": c.Model, "max_tokens": 64, "stream": true, "messages": []any{map[string]any{"role": "user", "content": "Reply exactly OK."}}}
		resp, err := messagesRequest(ctx, client, c, key, body)
		if err == nil {
			if resp.StatusCode == 200 && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
				text := ""
				err = readSSE(ctx, resp.Body, func(f []byte) error {
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
				_, err = readMessage(resp, key)
				if err == nil {
					err = errors.New("未返回 SSE 流")
				}
			}
			resp.Body.Close()
		}
		if err != nil {
			cap.Details = append(cap.Details, "流式响应："+redact(err.Error(), key))
		}
	}
	if ctx.Err() != nil {
		return cap, errors.New("Agent 能力验证已取消或超时；未更新连接能力")
	}
	for _, check := range []struct {
		label string
		ok    bool
	}{{"文本", cap.Text}, {"图片输入", cap.Vision}, {"工具调用", cap.Tools}, {"流式响应", cap.Streaming}} {
		if check.ok {
			cap.Details = append(cap.Details, check.label+"：通过")
		} else {
			cap.Details = append(cap.Details, check.label+"：未通过")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var current Connection
	if e = s.store.Get("connection", id, &current); e != nil {
		return cap, e
	}
	current = normalizeConnection(current)
	if current.BaseURL != c.BaseURL || current.Model != c.Model || current.Credential != c.Credential || current.Mode != c.Mode || current.AuthMode != c.AuthMode || current.MaxOutputTokens != c.MaxOutputTokens {
		return cap, errors.New("连接已修改，请重新验证")
	}
	current.Capabilities = cap
	return cap, s.store.Put("connection", id, current)
}
