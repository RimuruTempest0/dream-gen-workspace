package core

// SmartProgress describes the most recent smart-generation run while it still
// needs attention; RunID is empty once that run is finished.
type SmartProgress struct {
	RunID string `json:"runID"`
	// State: analyzing, needs_input, generating (images in the queue) or paused
	// (unsent images waiting for the user to resume).
	State string `json:"state"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
	// Step is the current step while analyzing (see RunProgress).
	Step string `json:"step,omitempty"`
}

// CurrentSmartProgress reports only the newest run, so an old abandoned request
// never pulls the user back; a finished newest run means "start a new request".
func (s *Service) CurrentSmartProgress() (SmartProgress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, e := List[AgentRun](s.store, "agent_run")
	if e != nil || len(runs) == 0 {
		return SmartProgress{}, e
	}
	r := runs[0]
	switch r.Status {
	case "analyzing", "needs_input":
		p := SmartProgress{RunID: r.ID, State: r.Status}
		if r.Progress != nil {
			p.Step = r.Progress.Step
		}
		return p, nil
	}
	if r.BatchID == "" {
		return SmartProgress{}, nil
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return SmartProgress{}, e
	}
	p := SmartProgress{RunID: r.ID}
	seen := map[string]bool{}
	running, paused := false, false
	for _, t := range tasks { // newest first: the first task of a slot is its current attempt
		if t.AgentRunID != r.ID || seen[t.PlanItemID] {
			continue
		}
		seen[t.PlanItemID] = true
		p.Total++
		switch {
		case t.Status == "succeeded":
			p.Done++
		case t.Status == "blocked":
			paused = true
		case active(t.Status):
			running = true
		}
	}
	switch {
	case running:
		p.State = "generating"
	case paused:
		p.State = "paused"
	default:
		return SmartProgress{}, nil
	}
	return p, nil
}
