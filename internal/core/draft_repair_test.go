package core

import (
	"strings"
	"testing"
)

func TestDraftRecoversFromDeletedConnection(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	old := saveConnection(t, s, "https://old.example.com/v1")
	in := agentInput(llm, old)
	in.ImageModel = "old-model"
	if e := s.SetAgentDraft(in); e != nil {
		t.Fatal(e)
	}
	// The user deletes the image connection and creates a new one.
	if e := s.DeleteConnection(old.ID); e != nil {
		t.Fatal(e)
	}
	replacement := saveConnection(t, s, "https://new.example.com/v1")
	draft, _ := s.GetAgentDraft()
	if draft.ImageConnectionID != replacement.ID || draft.ImageModel != "" || draft.LLMConnectionID != llm.ID {
		t.Fatalf("draft not repaired: %+v", draft)
	}
	// With several candidates the user chooses; starting then explains instead of a database error.
	saveConnection(t, s, "https://third.example.com/v1")
	draft.ImageConnectionID = old.ID
	s.SetAgentDraft(draft)
	draft, _ = s.GetAgentDraft()
	if draft.ImageConnectionID != "" {
		t.Fatal("ambiguous replacement chosen automatically", draft.ImageConnectionID)
	}
	in.ImageConnectionID = old.ID
	_, e := s.StartSmartGeneration(in, "deleted-connection")
	if e == nil || !strings.Contains(e.Error(), "生图中转站已删除") || strings.Contains(e.Error(), "no rows") {
		t.Fatal("deleted connection not explained", e)
	}
}
