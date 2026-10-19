package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInvalidKeyPausesBatchAndBulkRetryRecovers(t *testing.T) {
	var reject atomic.Bool
	var requests atomic.Int32
	reject.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if reject.Load() {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"message":"Invalid API key"}}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG())}}})
	}))
	defer server.Close()
	s := testService(t)
	if e := s.SetImageConcurrency(1); e != nil {
		t.Fatal(e)
	}
	var c Connection
	s.store.Get("connection", saveConnection(t, s, server.URL).ID, &c) // keep the encrypted key reference
	runID := NewID()
	tasks := []Task{}
	for i := 1; i <= 4; i++ {
		tasks = append(tasks, Task{ID: NewID(), BatchID: "batch-auth", AgentRunID: runID, PlanItemID: fmt.Sprintf("slot-%d", i), CreatedAt: Now(), UpdatedAt: Now(), Status: "queued", Prompt: "p", Model: "m", Size: "1024x1024", Connection: c, References: []string{}, AssetIDs: []string{}})
	}
	unknown := Task{ID: NewID(), BatchID: "batch-auth", AgentRunID: runID, PlanItemID: "slot-5", CreatedAt: Now(), UpdatedAt: Now(), Status: "unknown", Prompt: "p", Model: "m", Connection: c, References: []string{}, AssetIDs: []string{}}
	if _, e := s.store.Submit("auth-batch", "batch-auth", append(tasks, unknown)); e != nil {
		t.Fatal(e)
	}
	s.Start(func() {})
	waitFor(t, "first failure and pause", func() bool { return countStatus(s, "failed") == 1 && countStatus(s, "blocked") == 3 })
	if requests.Load() != 1 {
		t.Fatal("images after a rejected key were still sent", requests.Load())
	}
	current, _ := s.GetAgentTasks(runID)
	for _, task := range current {
		switch task.Status {
		case "failed":
			if !strings.HasPrefix(task.Error, "API Key 无效（HTTP 401）") {
				t.Fatal("unclear key error", task.Error)
			}
		case "blocked":
			if !strings.Contains(task.Error, "尚未提交、未计费") {
				t.Fatal("unclear pause reason", task.Error)
			}
		}
	}

	// The user fixes the key; one confirmed action regenerates the failed image and
	// resumes the paused ones, but never the image whose result is unknown.
	reject.Store(false)
	n, e := s.RetryFailedAgentImages(runID)
	if e != nil || n != 4 {
		t.Fatal("bulk retry", n, e)
	}
	waitFor(t, "all recovered", func() bool {
		current, _ := s.GetAgentTasks(runID)
		done := 0
		for _, task := range current {
			if task.Status == "succeeded" {
				done++
			}
		}
		return done == 4
	})
	if requests.Load() != 5 {
		t.Fatal("each image must be sent once more exactly", requests.Load())
	}
	current, _ = s.GetAgentTasks(runID)
	for _, task := range current {
		if task.PlanItemID == "slot-5" && task.Status != "unknown" {
			t.Fatal("unknown result was resent")
		}
	}
	if _, e := s.RetryFailedAgentImages(runID); e == nil {
		t.Fatal("second click should find nothing to retry")
	}
}

func TestRelayTimeoutIsExplained(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		w.WriteHeader(524)
		io.WriteString(w, "error code: 524")
	}))
	defer server.Close()
	s := testService(t)
	c := saveConnection(t, s, server.URL)
	if _, e := s.Submit(GenerateInput{RequestID: "timeout", ConnectionID: c.ID, Model: "m", Prompts: []string{"a"}, Count: 1, References: []string{}, Size: "auto", Quality: "auto"}); e != nil {
		t.Fatal(e)
	}
	s.stepNext()
	tasks, _ := List[Task](s.store, "task")
	if tasks[0].Status != "unknown" || !strings.HasPrefix(tasks[0].Error, "中转站等待约") || !strings.Contains(tasks[0].Error, "HTTP 524") || !strings.Contains(tasks[0].Error, "可能仍在中转站生成并已计费") {
		t.Fatal("relay timeout not explained", tasks[0].Status, tasks[0].Error)
	}
	if !relayTimedOut(GenerationDiagnostic{ElapsedMS: 116_000, NetworkError: "unexpected EOF"}) || relayTimedOut(GenerationDiagnostic{ElapsedMS: 5_000, NetworkError: "unexpected EOF"}) {
		t.Fatal("long disconnect classification wrong")
	}
}
