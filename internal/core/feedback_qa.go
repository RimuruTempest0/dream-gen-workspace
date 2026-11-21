//go:build desktopqa

package core

// SeedTestSuite saves a submitted suite of three images (one failed, one with an
// unknown result, one stopped) without generating anything, for the desktop QA
// of 问题反馈. stopped also marks the analysis as interrupted.
func (s *Service) SeedTestSuite(stopped bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: "submitted", Stage: "QA", BatchID: NewID(), SubmittedRevision: 1, Language: s.Language(),
		Input:     AgentInput{Instruction: "QA suite: worn front, flat lay, gift box", TargetCount: 3, Size: "1024x1024", Quality: "auto", References: []string{}, SkillIDs: []string{}},
		Questions: []AgentQuestion{}, Events: []AgentEvent{{At: Now(), Text: "QA plan saved"}}, Product: ProductInfo{ReferenceIDs: []string{}},
		Plans: []SuitePlan{{Revision: 1, CreatedAt: Now(), Locked: []string{"QA product"}, Editable: []string{}, Items: []SuiteItem{
			{ID: "qa-1", Purpose: "worn front", Scene: "studio", Prompt: "QA prompt 1", References: []string{}},
			{ID: "qa-2", Purpose: "flat lay", Scene: "white", Prompt: "QA prompt 2", References: []string{}},
			{ID: "qa-3", Purpose: "gift box", Scene: "table", Prompt: "QA prompt 3", References: []string{}},
		}}}}
	if stopped {
		run.Status, run.Error = "interrupted", "QA stop"
	}
	if e := s.store.Put("agent_run", run.ID, run); e != nil {
		return "", e
	}
	for i, status := range []string{"failed", "unknown", "canceled"} {
		item := run.Plans[0].Items[i]
		t := Task{ID: NewID(), BatchID: run.BatchID, AgentRunID: run.ID, PlanItemID: item.ID, CreatedAt: Now(), UpdatedAt: Now(), Status: status, Error: "QA " + status, Prompt: item.Prompt, Model: "qa-image", Size: "1024x1024", Quality: "auto", References: []string{}, AssetIDs: []string{}}
		if e := s.store.Put("task", t.ID, t); e != nil {
			return "", e
		}
	}
	return run.ID, nil
}
