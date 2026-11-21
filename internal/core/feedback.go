package core

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// 问题反馈 (#14): after checking a 套图 (one smart run) the tester writes down what
// was wrong and saves a 反馈包 on this computer, to send to the provider. The
// feedback is kept with the suite and can be changed and exported again.
//
// Verdicts and problem types are stored as fixed codes and shown in the
// interface language; the tester's own words are kept as written (ADR 0004).

// SuiteFeedback is the tester's verdict on one 套图. Items are the optional
// per-image verdicts (逐张核验, one per plan slot or version; feedback_items.go).
type SuiteFeedback struct {
	RunID     string         `json:"runID"`
	Verdict   string         `json:"verdict"` // feedbackVerdicts
	Problems  []string       `json:"problems"`
	Note      string         `json:"note"`
	Items     []FeedbackItem `json:"items,omitempty"`
	UpdatedAt string         `json:"updatedAt"`
}

// FeedbackItem is the verdict on one image of the suite: its plan slot and the
// task (version) looked at.
type FeedbackItem struct {
	PlanItemID string   `json:"planItemID,omitempty"`
	TaskID     string   `json:"taskID,omitempty"`
	Verdict    string   `json:"verdict"` // ok, problem
	Problems   []string `json:"problems,omitempty"`
	Note       string   `json:"note,omitempty"`
}

var (
	feedbackVerdicts = []string{"usable", "partly-usable", "unusable"}
	// feedbackProblems are the suite-level problem types, in the order shown.
	feedbackProblems = []string{"missing-shot", "too-similar", "design-inconsistent", "product-changed", "edit-missed", "interrupted", "other"}
)

// The tester's notes are limited in characters (code points), as the panel
// counts them: the note on the set, and the one sentence on an image.
const (
	maxFeedbackNote     = 8000
	maxFeedbackItemNote = 2000
)

// noteFits reports whether note is valid text of at most max characters.
func noteFits(note string, max int) bool {
	return utf8.ValidString(note) && utf8.RuneCountInString(note) <= max
}

// GetSuiteFeedback returns the saved feedback on the suite, or an empty one.
func (s *Service) GetSuiteFeedback(runID string) (SuiteFeedback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.suiteFeedbackLocked(runID)
}

func (s *Service) suiteFeedbackLocked(runID string) (SuiteFeedback, error) {
	var run AgentRun
	if e := s.store.Get("agent_run", runID, &run); e != nil {
		return SuiteFeedback{}, missing(e, s.msg("feedback.runMissing"))
	}
	v := SuiteFeedback{RunID: runID, Problems: []string{}}
	if s.store.Get("suite_feedback", runID, &v) == nil && v.Problems == nil {
		v.Problems = []string{}
	}
	return v, nil
}

// SaveSuiteFeedback keeps the feedback with its suite. The verdict is required;
// a suite that is not fully usable needs a problem type or a note.
func (s *Service) SaveSuiteFeedback(v SuiteFeedback) (SuiteFeedback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e := s.suiteFeedbackLocked(v.RunID); e != nil {
		return v, e
	}
	v.Note = strings.TrimSpace(strings.ReplaceAll(v.Note, "\r\n", "\n"))
	if !slices.Contains(feedbackVerdicts, v.Verdict) {
		return v, s.fail("feedback.verdictRequired")
	}
	problems := []string{}
	for _, p := range feedbackProblems {
		if slices.Contains(v.Problems, p) {
			problems = append(problems, p)
		}
	}
	if len(problems) != len(slices.Compact(slices.Sorted(slices.Values(v.Problems)))) {
		return v, s.fail("feedback.problemUnknown")
	}
	v.Problems = problems
	if !noteFits(v.Note, maxFeedbackNote) {
		return v, s.fail("feedback.noteTooLong", "max", maxFeedbackNote)
	}
	if v.Verdict != "usable" && len(v.Problems) == 0 && v.Note == "" {
		return v, s.fail("feedback.detailRequired")
	}
	items, e := s.cleanFeedbackItemsLocked(v.RunID, v.Items)
	if e != nil {
		return v, e
	}
	v.Items = items
	v.UpdatedAt = Now()
	return v, s.store.Put("suite_feedback", v.RunID, v)
}
