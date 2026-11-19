package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ---- images refused by the image model's content check ----------------------
//
// Since 0.6.28 a refused image is sent again automatically: first unchanged (the
// check is partly random, so the same request often passes), then once more with
// its prompt reworded by the LLM. A refused request generates nothing, so these
// resends normally cost nothing; only an image that is generated is billed.

var policyWords = regexp.MustCompile(`(?i)moderation|content[ _-]?(policy|filter|management)|safety[ _-]?(system|policy|check|filter)|policy[ _-]?violation|内容政策|内容安全|内容审核|安全审核|安全系统|安全政策|安全策略|敏感内容|违反.{0,12}(政策|规定|策略|规范)`)

// policyRejected recognises a request the image model's content check refused
// before generating. Relays answer it with a 4xx (usually 400) and the model's
// message; a refused key, an empty account and rate limits are told apart first.
func policyRejected(status int, message string) bool {
	return status >= 400 && status < 500 && status != 401 && status != 402 && status != 429 && policyWords.MatchString(message)
}

// policyRetryLocked queues the next automatic attempt for a refused image and
// returns the note for it.
func (s *Service) policyRetryLocked(t *Task) string {
	if t.PolicyStep >= 2 {
		return s.msg("generation.policy.gaveUp")
	}
	reword := t.PolicyStep == 1
	if reword {
		if _, _, ok := s.rewordLLMLocked(*t); !ok {
			return s.msg("generation.policy.noLLM")
		}
	}
	var live Connection
	if s.store.Get("connection", t.Connection.ID, &live) != nil {
		return s.msg("generation.retry.serviceDeleted")
	}
	next, e := s.queueAttemptLocked(*t, normalizeConnection(live), "")
	if e != nil {
		return s.msg("generation.policy.notQueued", "error", e.Error())
	}
	next.PolicyRetryOf, next.PolicyStep, next.Rewording = t.ID, t.PolicyStep+1, reword
	if e = s.store.Put("task", next.ID, next); e != nil {
		return s.msg("generation.policy.notQueued", "error", e.Error())
	}
	t.PolicyRetriedAs = next.ID
	if reword {
		_ = s.store.appendLog("warn", "image", t.BatchID, s.msg("generation.log.policyReword", "from", t.ID, "to", next.ID))
		s.startRewordLocked(next.ID)
		return s.msg("generation.policy.rewording")
	}
	_ = s.store.appendLog("warn", "image", t.BatchID, s.msg("generation.log.policyResend", "from", t.ID, "to", next.ID))
	return s.msg("generation.policy.resent")
}

// rewordLLMLocked picks the LLM line that rewrites a refused prompt: the smart
// generation's own line first, then the licence's main line, then any LLM line.
func (s *Service) rewordLLMLocked(t Task) (Connection, string, bool) {
	ids := []string{}
	if t.AgentRunID != "" {
		var run AgentRun
		if s.store.Get("agent_run", t.AgentRunID, &run) == nil {
			ids = append(ids, run.LLMConnection.ID, run.Input.LLMConnectionID)
		}
	}
	cs, _ := List[Connection](s.store, "connection")
	for _, managed := range []bool{true, false} {
		for _, c := range cs {
			if (c.Managed == "llm-1") == managed {
				ids = append(ids, c.ID)
			}
		}
	}
	for _, id := range ids {
		var c Connection
		if id == "" || s.store.Get("connection", id, &c) != nil {
			continue
		}
		c = normalizeConnection(c)
		if c.Kind != "llm" || c.Model == "" || (s.hostedLocked() && c.Managed == "") {
			continue
		}
		if key, e := s.key(c.Credential); e == nil && key != "" {
			return c, key, true
		}
	}
	return Connection{}, "", false
}

// startRewordLocked rewrites the prompt of a held attempt in the background. It
// also resumes an attempt left held when the app last exited.
func (s *Service) startRewordLocked(id string) {
	if s.rewording[id] || s.ctx.Err() != nil {
		return
	}
	s.rewording[id] = true
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		s.reword(id)
		s.mu.Lock()
		delete(s.rewording, id)
		s.mu.Unlock()
		s.signal()
	}()
}

const rewordTimeout = 3 * time.Minute

func (s *Service) reword(id string) {
	s.mu.Lock()
	var t Task
	if s.store.Get("task", id, &t) != nil || t.Status != "queued" || !t.Rewording {
		s.mu.Unlock()
		return
	}
	c, key, ok := s.rewordLLMLocked(t)
	if t.RewordFor == rewordMood {
		c, key, ok = s.moodLLMLocked(t)
	}
	s.mu.Unlock()
	prompt, e := "", s.fail("generation.noLLM")
	if ok {
		ctx, cancel := context.WithTimeout(s.ctx, rewordTimeout)
		if t.RewordFor == rewordMood {
			prompt, e = s.moodRewrite(ctx, c, key, t)
		} else if t.RewordFor == rewordRefine {
			prompt, e = s.refineRequirements(ctx, c, key, t)
		} else {
			prompt, e = s.rewordPrompt(ctx, c, key, t.Language, t.Prompt)
		}
		cancel()
	}
	if s.ctx.Err() != nil {
		return // the app is closing; the held attempt resumes on the next start
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store.Get("task", id, &t) != nil || t.Status != "queued" || !t.Rewording {
		return
	}
	t.Rewording, t.UpdatedAt = false, Now()
	if t.RewordFor == rewordMood {
		if e != nil {
			t.Status = "failed"
			t.Error = s.msg("generation.moodRewriteFailed", "error", s.redact(e.Error(), key))
			// The reason may quote in-image text of the prompt, so it stays on the task.
			_ = s.store.appendLog("warn", "image", t.BatchID, s.msg("generation.log.moodRewriteFailed", "task", t.ID))
		} else {
			t.Prompt = prompt
			// The prompt itself is not logged.
			_ = s.store.appendLog("info", "image", t.BatchID, s.msg("generation.log.moodRewritten"))
		}
		_ = s.store.Put("task", t.ID, t)
		return
	}
	if t.RewordFor == rewordRefine {
		if e != nil {
			t.Status = "failed"
			t.Error = s.msg("generation.refineRewriteFailed", "error", s.redact(e.Error(), key))
			_ = s.store.appendLog("warn", "agent", t.AgentRunID, s.msg("generation.log.refineRewriteFailed", "error", s.redact(e.Error(), key)))
		} else {
			t.Prompt = prompt
			_ = s.store.appendLog("info", "agent", t.AgentRunID, s.msg("generation.log.refineRewritten", "task", t.ID))
		}
		_ = s.store.Put("task", t.ID, t)
		return
	}
	if e != nil {
		t.Status = "failed"
		t.Error = s.msg("generation.policyRefused") + s.msg("generation.policy.rewordFailed", "error", s.redact(e.Error(), key))
		_ = s.store.appendLog("warn", "image", t.BatchID, s.msg("generation.log.policyRewordFailed", "error", s.redact(e.Error(), key)))
	} else {
		t.Prompt = prompt
		_ = s.store.appendLog("info", "image", t.BatchID, s.msg("generation.log.policyReworded", "task", t.ID))
	}
	_ = s.store.Put("task", t.ID, t)
}

const rewordInstruction = `图片模型的内容安全审核拒绝了下面这条电商生图提示词（多半是误判）。请改写措辞，让它能通过审核，同时画出的仍是同一张图。
1. 只改可能被误判的说法：身体、皮肤、裸露、贴身、性感等描述；儿童或婴儿与床、浴室、洗澡、睡衣、换衣等的组合；武器、血、伤害、暴力、毒品、酒精相关的词，包括 gun、shot、blood、bullet、nude、skin 这类颜色或材质名（改成普通的颜色或材质说法）；真实人物、名人、品牌、商标、知名角色或 IP 的名字（改成对外观的中性描述）。
2. 其余保持原样：每一行的用途、商品、设计、图案、颜色、尺寸数字、参考图编号和作用、构图、场景都不变；引号里的画面文字逐字保留，不翻译、不改大小写。
3. 不新增内容，不删去与上面无关的要求。
4. 只输出改写后的完整提示词，不要解释、标题、引号或代码块。

原提示词：
`

var quotedText = regexp.MustCompile(`“([^”]+)”|"([^"]+)"`)

// rewordPrompt asks the LLM for a reworded prompt. The sample line the backend
// added is kept as it is, and every quoted in-image text must survive verbatim.
func (s *Service) rewordPrompt(ctx context.Context, c Connection, key, lang, prompt string) (string, error) {
	head, body := "", prompt
	if hasSampleLead(prompt) {
		if line, rest, ok := strings.Cut(prompt, "\n"); ok {
			head, body = line+"\n", rest
		}
	}
	text, e := s.llmRewrite(ctx, c, key, withPromptLanguage(lang, rewordInstruction+body),s.msg("agent.llmStage.reword"), func(text string) error {
		for _, q := range quotedText.FindAllStringSubmatch(body, -1) {
			if want := q[1] + q[2]; !strings.Contains(text, want) {
				return s.fail("agent.rewrite.lostText", "text", want)
			}
		}
		return nil
	})
	if e != nil {
		return "", e
	}
	return head + text, nil
}

// llmRewrite sends one rewriting request to the LLM and returns its text after
// check accepts it. The call is recorded in the LLM monitor under stage.
func (s *Service) llmRewrite(ctx context.Context, c Connection, key, request, stage string, check func(string) error) (string, error) {
	return s.llmAsk(ctx, c, key, "rewrite", stage, request, check)
}

// llmAsk sends one non-streaming request (text, or content blocks with images) and
// returns the reply text after check accepts it; the call is recorded as kind.
// A connection dropped before any answer is tried once more: asking again sends
// no image and costs nothing at the image service. Each attempt is recorded.
func (s *Service) llmAsk(ctx context.Context, c Connection, key, kind, stage string, content any, check func(string) error) (string, error) {
	text, dropped, e := s.llmAskOnce(ctx, c, key, kind, stage, content, check)
	if dropped && ctx.Err() == nil {
		text, _, e = s.llmAskOnce(ctx, c, key, kind, stage, content, check)
	}
	return text, e
}

// llmAskOnce is one attempt of llmAsk; dropped reports a connection that failed
// before any HTTP answer arrived.
func (s *Service) llmAskOnce(ctx context.Context, c Connection, key, kind, stage string, content any, check func(string) error) (text string, dropped bool, err error) {
	ctx = s.langCtx(ctx)
	req := map[string]any{
		"model": c.Model,
		// The line's own limit: a reasoning model spends output tokens on thinking
		// first, and a fixed 8192 ran out before it wrote any text.
		"max_tokens": LLMOutputLimit(c.MaxOutputTokens),
		"messages":   []any{map[string]any{"role": "user", "content": content}},
		// Streamed: a model that thinks first sends nothing for minutes otherwise,
		// and a relay drops the idle connection (幻灵 11/17: EOF after 60 s).
		"stream": true,
	}
	if c.Mode == "compatible" {
		basicMessages(req)
	}
	call := s.startLLMCall(kind, "", c, nil)
	call.Stage = stage
	started := time.Now()
	finish := func(e error) error {
		if e != nil {
			call.Status, call.Error = "error", s.redact(e.Error(), key)
		} else {
			call.Status = "completed"
		}
		s.saveLLMCall(call, started)
		return e
	}
	resp, e := messagesRequest(ctx, llmHTTPClient(), c, key, req)
	if e != nil {
		return "", true, finish(s.fail("connection.llm.connectFailed", "error", e.Error()))
	}
	call.HTTPStatus = resp.StatusCode
	call.RequestID = resp.Header.Get("request-id")
	if call.RequestID == "" {
		call.RequestID = resp.Header.Get("x-request-id")
	}
	var stopReason string
	if resp.StatusCode == 200 && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		call.Stream = true
		text, stopReason, e = readTextStream(ctx, resp, &call, started)
		if e != nil {
			return "", false, finish(s.fail("connection.llm.replyCut", "error", e.Error()))
		}
	} else {
		// A relay that ignores "stream" answers with one JSON message.
		m, e := readMessage(resp, key, s.Language())
		if e != nil {
			return "", false, finish(e)
		}
		call.addUsage(m.Usage)
		for _, b := range m.Content {
			if b.Type == "text" {
				text += b.Text
			}
		}
		stopReason = m.StopReason
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text[strings.Index(text+"\n", "\n"):]), "```"))
	}
	if text == "" {
		if stopReason == "max_tokens" {
			return "", false, finish(s.fail("connection.llm.thinkingUsedLimit", "limit", LLMOutputLimit(c.MaxOutputTokens)))
		}
		return "", false, finish(s.fail("connection.llm.emptyRewrite"))
	}
	if e := check(text); e != nil {
		return "", false, finish(e)
	}
	if call.FirstByteMS == 0 {
		call.FirstByteMS = time.Since(started).Milliseconds()
	}
	return text, false, finish(nil)
}

// readTextStream reads a streamed Messages reply: the text of its text blocks
// (thinking is left out) and its stop reason, with usage added to call.
func readTextStream(ctx context.Context, resp *http.Response, call *LLMCall, started time.Time) (string, string, error) {
	defer resp.Body.Close()
	var text strings.Builder
	stop := ""
	e := readSSE(ctx, resp.Body, func(frame []byte) error {
		if call.FirstByteMS == 0 {
			call.FirstByteMS = max(time.Since(started).Milliseconds(), 1)
		}
		call.readStreamUsage(frame)
		for _, line := range strings.Split(string(frame), "\n") {
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var v struct {
				Type  string `json:"type"`
				Delta struct {
					Type       string `json:"type"`
					Text       string `json:"text"`
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &v) != nil {
				continue
			}
			switch {
			case v.Type == "error":
				return errors.New(v.Error.Message)
			case v.Type == "content_block_delta" && v.Delta.Type == "text_delta":
				text.WriteString(v.Delta.Text)
			case v.Type == "message_delta" && v.Delta.StopReason != "":
				stop = v.Delta.StopReason
			}
		}
		return nil
	})
	return text.String(), stop, e
}
