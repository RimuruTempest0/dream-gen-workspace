package core

import (
	"context"
	"errors"
	"fmt"
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

var policyWords = regexp.MustCompile(`(?i)moderation|content[ _-]?(policy|filter|management)|safety[ _-]?(system|policy|check|filter)|policy[ _-]?violation|内容政策|内容安全|内容审核|安全审核|安全系统|敏感内容|违反.{0,12}(政策|规定|策略|规范)`)

// policyRejected recognises a request the image model's content check refused
// before generating. Relays answer it with a 4xx (usually 400) and the model's
// message; a refused key, an empty account and rate limits are told apart first.
func policyRejected(status int, message string) bool {
	return status >= 400 && status < 500 && status != 401 && status != 402 && status != 429 && policyWords.MatchString(message)
}

const policyRefusedPrefix = "生图模型的内容审核拒绝了这张图"

// policyRetryLocked queues the next automatic attempt for a refused image and
// returns the note for it.
func (s *Service) policyRetryLocked(t *Task) string {
	if t.PolicyStep >= 2 {
		return "自动改写措辞后仍被拒绝，不再自动重发。请修改提示词，或换掉可能被误判的参考图后重新生成。"
	}
	reword := t.PolicyStep == 1
	if reword {
		if _, _, ok := s.rewordLLMLocked(*t); !ok {
			return "原样重发后仍被拒绝；没有可用的 LLM 来自动改写措辞，请修改提示词后重新生成。"
		}
	}
	var live Connection
	if s.store.Get("connection", t.Connection.ID, &live) != nil {
		return "原生图中转站已删除，未自动重发。"
	}
	next, e := s.queueAttemptLocked(*t, normalizeConnection(live), "")
	if e != nil {
		return "自动重发未能排队：" + e.Error()
	}
	next.PolicyRetryOf, next.PolicyStep, next.Rewording = t.ID, t.PolicyStep+1, reword
	if e = s.store.Put("task", next.ID, next); e != nil {
		return "自动重发未能排队：" + e.Error()
	}
	t.PolicyRetriedAs = next.ID
	if reword {
		_ = s.store.appendLog("warn", "image", t.BatchID, fmt.Sprintf("内容审核再次拒绝，用 LLM 改写措辞后自动重发：%s → %s", t.ID, next.ID))
		s.startRewordLocked(next.ID)
		return "原样重发后仍被拒绝，正在用 LLM 改写这张图的措辞后再发一次（商品设计、画面文字和参考图不变）。"
	}
	_ = s.store.appendLog("warn", "image", t.BatchID, fmt.Sprintf("内容审核拒绝，自动原样重发一次：%s → %s", t.ID, next.ID))
	return "已自动原样重发一次（内容审核有一定随机性，常常重发就能通过）。"
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
		if c.Kind != "llm" || c.Model == "" {
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
	s.mu.Unlock()
	prompt, e := "", errors.New("没有可用的 LLM")
	if ok {
		ctx, cancel := context.WithTimeout(s.ctx, rewordTimeout)
		prompt, e = s.rewordPrompt(ctx, c, key, t.Prompt)
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
	if e != nil {
		t.Status = "failed"
		t.Error = fmt.Sprintf("%s，原样重发后仍被拒绝；自动改写措辞没有成功（%s）。这次没有发送、不计费。请修改提示词后重新生成。", policyRefusedPrefix, redact(e.Error(), key))
		_ = s.store.appendLog("warn", "image", t.BatchID, "内容审核拒绝后自动改写措辞失败："+redact(e.Error(), key))
	} else {
		t.Prompt = prompt
		_ = s.store.appendLog("info", "image", t.BatchID, "已改写内容审核拒绝的提示词，自动重发："+t.ID)
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
func (s *Service) rewordPrompt(ctx context.Context, c Connection, key, prompt string) (string, error) {
	head, body := "", prompt
	if strings.HasPrefix(prompt, "样板图：参考图第") {
		if line, rest, ok := strings.Cut(prompt, "\n"); ok {
			head, body = line+"\n", rest
		}
	}
	req := map[string]any{
		"model":      c.Model,
		"max_tokens": 8192,
		"messages":   []any{map[string]any{"role": "user", "content": rewordInstruction + body}},
	}
	if c.Mode == "compatible" {
		basicMessages(req)
	}
	call := s.startLLMCall("rewrite", "", c, nil)
	call.Stage = "内容审核拒绝后改写提示词"
	started := time.Now()
	finish := func(e error) error {
		if e != nil {
			call.Status, call.Error = "error", redact(e.Error(), key)
		} else {
			call.Status = "completed"
		}
		s.saveLLMCall(call, started)
		return e
	}
	resp, e := messagesRequest(ctx, llmHTTPClient(), c, key, req)
	if e != nil {
		return "", finish(errors.New("连接 LLM 失败：" + e.Error()))
	}
	call.HTTPStatus = resp.StatusCode
	call.RequestID = resp.Header.Get("request-id")
	if call.RequestID == "" {
		call.RequestID = resp.Header.Get("x-request-id")
	}
	m, e := readMessage(resp, key)
	if e != nil {
		return "", finish(e)
	}
	call.addUsage(m.Usage)
	text := ""
	for _, b := range m.Content {
		if b.Type == "text" {
			text += b.Text
		}
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text[strings.Index(text+"\n", "\n"):]), "```"))
	}
	if text == "" {
		return "", finish(errors.New("LLM 没有返回改写后的提示词"))
	}
	for _, q := range quotedText.FindAllStringSubmatch(body, -1) {
		if want := q[1] + q[2]; !strings.Contains(text, want) {
			return "", finish(fmt.Errorf("改写结果没有保留画面文字“%s”", want))
		}
	}
	call.FirstByteMS = time.Since(started).Milliseconds()
	return head + text, finish(nil)
}
