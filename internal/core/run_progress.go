package core

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Steps of a run's analysis, in their usual order. The Agent chooses its own order,
// so a run can return to an earlier step.
const (
	stepPrepare    = "prepare"
	stepProduct    = "product"
	stepImages     = "images"
	stepReferences = "references"
	stepPlan       = "plan"
	stepSubmit     = "submit"
)

// RunProgress is what the customer sees while a run is analyzed. It never goes to
// the LLM. The Reply fields are not saved: they change with every stream event, so
// they live in memory and are added when the run is read.
type RunProgress struct {
	Step             string   `json:"step"`
	StepStartedAt    string   `json:"stepStartedAt"`
	Visited          []string `json:"visited"`
	ProductVia       string   `json:"productVia,omitempty"` // direct or browser
	ProductBrowserMS int64    `json:"productBrowserMS,omitempty"`
	ImagesFound      int      `json:"imagesFound,omitempty"`
	ImagesSaved      int      `json:"imagesSaved,omitempty"`
	ReferencesSeen   int      `json:"referencesSeen,omitempty"`
	PlanWritten      int      `json:"planWritten,omitempty"`
	AgentVersion     string   `json:"agentVersion,omitempty"`
	LLMRequests      int      `json:"llmRequests,omitempty"`
	ContextTokens    int64    `json:"contextTokens,omitempty"`
	OutputTokens     int64    `json:"outputTokens,omitempty"`
	LineNote         string   `json:"lineNote,omitempty"`
	ProductDone      bool     `json:"productDone,omitempty"`
	ReplyThinking    bool     `json:"replyThinking,omitempty"`
	ReplyActive      bool     `json:"replyActive,omitempty"`
	ReplyChars       int      `json:"replyChars,omitempty"`
	ReplyLastAt      string   `json:"replyLastAt,omitempty"`
}

func newRunProgress() *RunProgress {
	return &RunProgress{Step: stepPrepare, StepStartedAt: Now(), Visited: []string{stepPrepare}}
}

// progressForAttempt starts an attempt's progress. A continued run does not read the
// product page again, so what it already saved shows as done.
func progressForAttempt(r AgentRun) *RunProgress {
	p := newRunProgress()
	if r.Product.SourceURL != "" && r.Product.Error == "" {
		p.visit(stepProduct)
		if n := len(r.Product.ReferenceIDs); n > 0 {
			p.visit(stepImages)
			p.ImagesFound, p.ImagesSaved = n, n
		}
	}
	return p
}

func (p *RunProgress) visit(step string) {
	for _, v := range p.Visited {
		if v == step {
			return
		}
	}
	p.Visited = append(p.Visited, step)
}

// enter makes step the current one; re-entering the current step keeps its timer.
func (p *RunProgress) enter(step string) {
	p.visit(step)
	if p.Step != step {
		p.Step, p.StepStartedAt = step, Now()
	}
}

// progressOf returns the run's progress, creating it for runs saved before 0.6.23.
func progressOf(r *AgentRun) *RunProgress {
	if r.Progress == nil {
		r.Progress = newRunProgress()
	}
	return r.Progress
}

func (s *Service) setProgress(id string, fn func(*RunProgress)) {
	_ = s.updateAgent(id, "", func(r *AgentRun) error {
		fn(progressOf(r))
		return nil
	})
}

// liveReply is the LLM reply being received for a run.
type liveReply struct {
	active   bool
	chars    int
	thinking bool
	lastAt   string
}

func (s *Service) replyStart(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replies == nil {
		s.replies = map[string]*liveReply{}
	}
	s.replies[id] = &liveReply{active: true, lastAt: Now()}
}

func (s *Service) replyEnd(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.replies, id)
}

func (s *Service) replyAdd(id string, chars int, thinking bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v := s.replies[id]; v != nil {
		v.chars += chars
		v.thinking = thinking
		v.lastAt = Now()
	}
}

// withLiveReply adds the reply being received to a run read for the UI.
func (s *Service) withLiveReply(r *AgentRun) {
	if v := s.replies[r.ID]; v != nil && r.Progress != nil {
		p := *r.Progress
		p.ReplyActive, p.ReplyChars, p.ReplyLastAt, p.ReplyThinking = v.active, v.chars, v.lastAt, v.thinking
		r.Progress = &p
	}
}

// replyFrame is what one stream event says about the reply. Events are read only
// for this; what the model wrote is not kept.
type replyFrame struct {
	chars       int
	thinking    bool // the characters were the model's reasoning
	writingPlan bool
	hitLimit    bool // the reply stopped at the output limit
}

func readReplyFrame(frame []byte) replyFrame {
	var out replyFrame
	for _, line := range strings.Split(string(frame), "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var v struct {
			ContentBlock struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta struct {
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &v) != nil {
			continue
		}
		out.chars += utf8.RuneCountInString(v.Delta.Text) + utf8.RuneCountInString(v.Delta.Thinking) + utf8.RuneCountInString(v.Delta.PartialJSON)
		if v.Delta.Thinking != "" {
			out.thinking = true
		}
		if v.ContentBlock.Type == "tool_use" && strings.HasSuffix(v.ContentBlock.Name, "save_suite_plan") {
			out.writingPlan = true
		}
		if v.Delta.StopReason == "max_tokens" {
			out.hitLimit = true
		}
	}
	return out
}
