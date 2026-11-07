package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ActivityEntry is one line of a run's activity feed: what Claude Code and the LLM
// did. Prompts and skill text are never written here, only names, counts and the
// Agent's own short messages.
type ActivityEntry struct {
	At   string `json:"at"`
	Kind string `json:"kind"` // agent, tool, llm, warn
	Text string `json:"text"`
}

const (
	maxActivityEntries = 300
	maxActivityText    = 300
)

// A request that has sent nothing for this long is treated as stuck. The slowest
// first reply on record that still succeeded took 71 seconds.
var llmFirstReplyTimeout = 2 * time.Minute

// agentAttemptLimit is how long one attempt may run. A reply that uses the whole output
// limit takes about 4 minutes, so 10 minutes left no room for one retry.
var agentAttemptLimit = 20 * time.Minute

// llmFailureReason says in plain words why a request failed.
func llmFailureReason(message string) string {
	switch {
	case strings.Contains(message, "TLS handshake timeout"):
		return "网络握手超时，请检查代理或网络"
	case strings.Contains(message, "unexpected EOF"), strings.Contains(message, "connection reset"), strings.Contains(message, "forcibly closed"):
		return "连接被中途切断（网络、代理或服务方）"
	case strings.Contains(message, "first-reply limit"):
		return "一直没有回复"
	case strings.Contains(message, "reasoning setting"):
		return "服务方不接受关闭思考，已改回默认"
	case strings.Contains(message, "deadline exceeded"):
		return "已到本次制作的时间上限"
	}
	return ""
}

// toolErrorText describes a failed tool call; a tool result is a string or a list of blocks.
func toolErrorText(content json.RawMessage, clean func(string) string) string {
	var text string
	if json.Unmarshal(content, &text) != nil {
		var blocks []struct {
			Text string `json:"text"`
		}
		json.Unmarshal(content, &blocks)
		for _, b := range blocks {
			text += b.Text
		}
	}
	if strings.Contains(text, "could not be parsed as JSON") {
		return "工具内容不完整：模型回复到达输出上限被截断，Agent 会重写"
	}
	if strings.Contains(strings.ToLower(text), "timed out") {
		return "工具调用超时（超过 60 秒没有结果），Agent 会自行处理"
	}
	text = clean(strings.TrimSpace(text))
	if utf8.RuneCountInString(text) > 80 {
		text = string([]rune(text)[:80]) + "…"
	}
	if text == "" {
		return "工具调用没有成功，Agent 会自行处理"
	}
	return "工具调用没有成功，Agent 会自行处理：" + text
}

func cutActivityText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) > maxActivityText {
		return string([]rune(text)[:maxActivityText]) + "…"
	}
	return text
}

func (s *Service) addActivity(id, kind, text string) {
	text = cutActivityText(text)
	if text == "" {
		return
	}
	_ = s.updateAgent(id, "", func(r *AgentRun) error {
		r.Activity = append(r.Activity, ActivityEntry{At: Now(), Kind: kind, Text: text})
		if len(r.Activity) > maxActivityEntries {
			r.Activity = r.Activity[len(r.Activity)-maxActivityEntries:]
		}
		return nil
	})
}

var skillNames = map[string]string{
	"product-analysis": "商品与参考图分析",
	"listing-suite":    "电商套图规划",
	"embroidery-rules": "刺绣定制约束",
	"p004-rules":       "P004 生图规则",
	"image-prompting":  "生图提示词写法",
}

// toolActivity describes a tool call without its content: a plan's prompts and a
// skill's rules stay out of the feed.
func toolActivity(name string, input json.RawMessage) string {
	var v struct {
		AssetID   string            `json:"assetID"`
		Skill     string            `json:"skill"`
		Name      string            `json:"name"`
		Items     []json.RawMessage `json:"items"`
		Questions []json.RawMessage `json:"questions"`
		Answers   []struct {
			Basis string `json:"basis"`
		} `json:"answers"`
		Append bool   `json:"append"`
		Label  string `json:"label"`
	}
	json.Unmarshal(input, &v)
	switch strings.TrimPrefix(name, "mcp__workbench__") {
	case "get_run_context":
		return "读取制作需求"
	case "fetch_product":
		return "读取商品页资料"
	case "read_reference_image":
		if len(v.AssetID) >= 8 {
			return "查看参考图 " + v.AssetID[:8]
		}
		return "查看参考图"
	case "read_skill_attachment":
		return "读取技能附件"
	case "request_information":
		return fmt.Sprintf("提出 %d 个补充问题", len(v.Questions))
	case "crop_reference":
		if v.Label != "" {
			return "截取参考图局部：" + v.Label
		}
		return "截取参考图局部"
	case "answer_questions":
		verify := 0
		for _, a := range v.Answers {
			if a.Basis == "verify" {
				verify++
			}
		}
		if verify > 0 {
			return fmt.Sprintf("自行回答 %d 个问题（其中 %d 项需核对）", len(v.Answers), verify)
		}
		return fmt.Sprintf("自行回答 %d 个问题", len(v.Answers))
	case "save_suite_plan":
		if v.Append {
			return fmt.Sprintf("暂存套图方案：本批 %d 张", len(v.Items))
		}
		return fmt.Sprintf("保存套图方案：本批 %d 张", len(v.Items))
	case "Skill":
		if label := skillNames[v.Skill]; label != "" {
			return "加载技能：" + label
		}
		return "加载技能"
	}
	return "调用工具"
}

// noteAgentEvent turns one line of Claude Code's event stream into feed entries.
// clean removes secrets from anything the Agent wrote.
func (s *Service) noteAgentEvent(id string, line []byte, clean func(string) string) {
	var event struct {
		Type       string  `json:"type"`
		Subtype    string  `json:"subtype"`
		Version    string  `json:"claude_code_version"`
		Model      string  `json:"model"`
		Tools      []any   `json:"tools"`
		Attempt    int     `json:"attempt"`
		NumTurns   int     `json:"num_turns"`
		DurationMS float64 `json:"duration_ms"`
		Message    struct {
			Content []struct {
				Type    string          `json:"type"`
				Text    string          `json:"text"`
				Name    string          `json:"name"`
				Input   json.RawMessage `json:"input"`
				Content json.RawMessage `json:"content"`
				IsError bool            `json:"is_error"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &event) != nil {
		return
	}
	switch event.Type {
	case "system":
		switch event.Subtype {
		case "init":
			version := event.Version
			s.addActivity(id, "agent", fmt.Sprintf("Claude Code %s 已启动 · 模型 %s · %d 个工具", version, clean(event.Model), len(event.Tools)))
			s.setProgress(id, func(p *RunProgress) { p.AgentVersion = version })
		case "api_retry":
			s.addActivity(id, "warn", fmt.Sprintf("Claude Code 正在重试 LLM 请求（第 %d 次）", event.Attempt))
		}
	case "assistant":
		for _, block := range event.Message.Content {
			switch block.Type {
			case "text":
				s.addActivity(id, "agent", clean(block.Text))
			case "tool_use":
				s.addActivity(id, "tool", toolActivity(block.Name, block.Input))
			}
		}
	case "user":
		for _, block := range event.Message.Content {
			if block.Type == "tool_result" && block.IsError {
				s.addActivity(id, "warn", toolErrorText(block.Content, clean))
			}
		}
	case "result":
		s.addActivity(id, "agent", fmt.Sprintf("Claude Code 本轮结束 · %d 个回合 · 用时 %s", event.NumTurns, spanText(int64(event.DurationMS))))
	}
}

func spanText(ms int64) string {
	seconds := ms / 1000
	if seconds < 60 {
		return fmt.Sprintf("%d 秒", seconds)
	}
	return fmt.Sprintf("%d 分 %d 秒", seconds/60, seconds%60)
}

func groupDigits(n int64) string {
	text := fmt.Sprint(n)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

// noteLLMCall records a finished LLM request in the feed and the running totals.
func (s *Service) noteLLMCall(id string, number int, line string, call LLMCall, started time.Time) {
	total := time.Since(started).Milliseconds()
	kind, text := "llm", ""
	switch call.Status {
	case "completed":
		text = fmt.Sprintf("LLM 请求 #%d 完成 · %s · 输入 %s · 输出 %s tokens · 首字 %s · 共 %s", number, line, groupDigits(call.InputTokens), groupDigits(call.OutputTokens), spanText(call.FirstByteMS), spanText(total))
	case "dropped":
		kind, text = "warn", fmt.Sprintf("LLM 请求 #%d 连接中断 · %s · 已等 %s", number, line, spanText(total))
	default:
		kind, text = "warn", fmt.Sprintf("LLM 请求 #%d 没有成功 · %s · 已等 %s", number, line, spanText(total))
		if call.HTTPStatus != 0 {
			text += fmt.Sprintf(" · HTTP %d", call.HTTPStatus)
		}
	}
	if reason := llmFailureReason(call.Error); reason != "" {
		text += " · " + reason
	}
	s.addActivity(id, kind, text)
	s.setProgress(id, func(p *RunProgress) {
		p.LLMRequests++
		p.OutputTokens += call.OutputTokens
		if call.InputTokens > 0 {
			p.ContextTokens = call.InputTokens
		}
	})
}

// backupLLMLine returns the other LLM line of a customer licence. Trial use stays on
// the main line, so backup keys are spent only after activation. The line must have
// passed the image check: a request can carry the reference images already viewed.
func (s *Service) backupLLMLine(current Connection) (Connection, string, bool) {
	want := map[string]string{"llm-1": "llm-2", "llm-2": "llm-1"}[current.Managed]
	s.mu.Lock()
	defer s.mu.Unlock()
	if want == "" || s.lic.mode == "trial" {
		return Connection{}, "", false
	}
	lines, e := List[Connection](s.store, "connection")
	if e != nil {
		return Connection{}, "", false
	}
	for _, c := range lines {
		if c.Managed != want || !c.Capabilities.Vision {
			continue
		}
		key, e := s.key(c.Credential)
		if e != nil || key == "" {
			return Connection{}, "", false
		}
		return normalizeConnection(c), key, true
	}
	return Connection{}, "", false
}

const noReplyStopped = "模型一直没有回复，本次分析已停止。资料已保存，点“继续制作”即可再试。"

// stopForNoReply ends the attempt with a clear reason instead of waiting out its limit.
func (s *Service) stopForNoReply(id string) {
	_ = s.updateAgent(id, "", func(r *AgentRun) error {
		r.StopReason = noReplyStopped
		return nil
	})
	s.mu.Lock()
	cancel := s.agentCancel
	running := s.agentRunID == id
	s.mu.Unlock()
	if running && cancel != nil {
		cancel()
	}
}

// interruptedMessage says why an attempt ended early, and mentions a plan only when
// one exists.
func interruptedMessage(r AgentRun) string {
	if r.StopReason != "" {
		return r.StopReason
	}
	saved := "资料已保存"
	if len(r.Plans) > 0 {
		saved = "资料和方案已保存"
	}
	if r.CancelRequested {
		return "分析已取消，" + saved + "，可继续制作。"
	}
	return fmt.Sprintf("分析超过 %d 分钟仍未完成，已停止，%s，可继续制作。", int(agentAttemptLimit/time.Minute), saved)
}
