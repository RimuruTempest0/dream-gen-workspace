package core

import (
	"imageworkbench/internal/i18n"
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
func llmFailureReason(lang, message string) string {
	switch {
	case strings.Contains(message, "TLS handshake timeout"):
		return i18n.T(lang, "agent.llmFailure.tls")
	case strings.Contains(message, "client connection lost"):
		return i18n.T(lang, "agent.llmFailure.lost")
	case strings.Contains(message, "unexpected EOF"), strings.Contains(message, "connection reset"), strings.Contains(message, "forcibly closed"):
		return i18n.T(lang, "agent.llmFailure.cut")
	case strings.Contains(message, "first-reply limit"):
		return i18n.T(lang, "agent.llmFailure.noReply")
	case strings.Contains(message, "reasoning setting"):
		return i18n.T(lang, "agent.llmFailure.reasoning")
	case strings.Contains(message, "deadline exceeded"):
		return i18n.T(lang, "agent.llmFailure.deadline")
	}
	return ""
}

// toolErrorText describes a failed tool call; a tool result is a string or a list of blocks.
func toolErrorText(lang string, content json.RawMessage, clean func(string) string) string {
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
		return i18n.T(lang, "agent.toolError.truncated")
	}
	if strings.Contains(strings.ToLower(text), "timed out") {
		return i18n.T(lang, "agent.toolError.timeout")
	}
	text = clean(strings.TrimSpace(text))
	if utf8.RuneCountInString(text) > 80 {
		text = string([]rune(text)[:80]) + "…"
	}
	if text == "" {
		return i18n.T(lang, "agent.toolError.failed")
	}
	return i18n.T(lang, "agent.toolError.failedWith", "error", text)
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

// skillNames are the catalog keys of the shipped skills' names in the feed.
var skillNames = map[string]string{
	"product-analysis": "agent.skill.productAnalysis",
	"listing-suite":    "agent.skill.listingSuite",
	"embroidery-rules": "agent.skill.embroideryRules",
	"p004-rules":       "agent.skill.p004Rules",
	"image-prompting":  "agent.skill.imagePrompting",
}

// toolActivity describes a tool call without its content: a plan's prompts and a
// skill's rules stay out of the feed.
func toolActivity(lang, name string, input json.RawMessage) string {
	t := func(key string, args ...any) string { return i18n.T(lang, key, args...) }
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
		return t("agent.activity.context")
	case "fetch_product":
		return t("agent.activity.product")
	case "read_reference_image":
		if len(v.AssetID) >= 8 {
			return t("agent.activity.referenceID", "id", v.AssetID[:8])
		}
		return t("agent.activity.reference")
	case "read_skill_attachment":
		return t("agent.activity.attachment")
	case "request_information":
		return t("agent.activity.questions", "n", len(v.Questions))
	case "crop_reference":
		if v.Label != "" {
			return t("agent.activity.cropLabel", "label", v.Label)
		}
		return t("agent.activity.crop")
	case "answer_questions":
		verify := 0
		for _, a := range v.Answers {
			if a.Basis == "verify" {
				verify++
			}
		}
		if verify > 0 {
			return t("agent.activity.answersVerify", "n", len(v.Answers), "verify", verify)
		}
		return t("agent.activity.answers", "n", len(v.Answers))
	case "save_suite_plan":
		if v.Append {
			return t("agent.activity.planStaged", "n", len(v.Items))
		}
		return t("agent.activity.planSaved", "n", len(v.Items))
	case "Skill":
		if key := skillNames[v.Skill]; key != "" {
			return t("agent.activity.skillNamed", "name", t(key))
		}
		return t("agent.activity.skill")
	}
	return t("agent.activity.tool")
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
			s.addActivity(id, "agent", s.msg("agent.activity.started", "version", version, "model", clean(event.Model), "tools", len(event.Tools)))
			s.setProgress(id, func(p *RunProgress) { p.AgentVersion = version })
		case "api_retry":
			s.addActivity(id, "warn", s.msg("agent.activity.retry", "n", event.Attempt))
		}
	case "assistant":
		for _, block := range event.Message.Content {
			switch block.Type {
			case "text":
				s.addActivity(id, "agent", clean(block.Text))
			case "tool_use":
				s.addActivity(id, "tool", toolActivity(s.Language(), block.Name, block.Input))
			}
		}
	case "user":
		for _, block := range event.Message.Content {
			if block.Type == "tool_result" && block.IsError {
				s.addActivity(id, "warn", toolErrorText(s.Language(), block.Content, clean))
			}
		}
	case "result":
		s.addActivity(id, "agent", s.msg("agent.activity.finished", "turns", event.NumTurns, "time", spanText(s.Language(), int64(event.DurationMS))))
	}
}

func spanText(lang string, ms int64) string {
	seconds := ms / 1000
	if seconds < 60 {
		return i18n.T(lang, "agent.span.seconds", "s", seconds)
	}
	return i18n.T(lang, "agent.span.minutes", "m", seconds/60, "s", seconds%60)
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
	kind, text, lang := "llm", "", s.Language()
	switch call.Status {
	case "completed":
		text = s.msg("agent.llmCall.completed", "n", number, "line", line, "input", groupDigits(call.InputTokens), "output", groupDigits(call.OutputTokens), "first", spanText(lang, call.FirstByteMS), "total", spanText(lang, total))
	case "dropped":
		kind, text = "warn", s.msg("agent.llmCall.dropped", "n", number, "line", line, "waited", spanText(lang, total))
	default:
		kind, text = "warn", s.msg("agent.llmCall.failed", "n", number, "line", line, "waited", spanText(lang, total))
		if call.HTTPStatus != 0 {
			text += fmt.Sprintf(" · HTTP %d", call.HTTPStatus)
		}
	}
	if reason := llmFailureReason(lang, call.Error); reason != "" {
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

// noReplyStopped is why an attempt stopped when the model never replied.
func (s *Service) noReplyStopped() string { return s.msg("agent.noReplyStopped") }

// stopForNoReply ends the attempt with a clear reason instead of waiting out its limit.
func (s *Service) stopForNoReply(id string) {
	_ = s.updateAgent(id, "", func(r *AgentRun) error {
		r.StopReason = s.noReplyStopped()
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
func interruptedMessage(lang string, r AgentRun) string {
	if r.StopReason != "" {
		return r.StopReason
	}
	saved := i18n.T(lang, "agent.interrupted.saved")
	if len(r.Plans) > 0 {
		saved = i18n.T(lang, "agent.interrupted.savedPlan")
	}
	if r.CancelRequested {
		return i18n.T(lang, "agent.interrupted.canceled", "saved", saved)
	}
	return i18n.T(lang, "agent.interrupted.timeout", "minutes", int(agentAttemptLimit/time.Minute), "saved", saved)
}
