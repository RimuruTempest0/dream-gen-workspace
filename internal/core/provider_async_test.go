package core

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// asyncRelay is a mock async-jobs upstream: a submit returns a job, polls answer
// "running" pending times and then the result (or a failure).
type asyncRelay struct {
	mu       sync.Mutex
	submits  int
	polls    int
	pending  int
	fail     bool
	idemKeys []string
	async    []bool
}

func (a *asyncRelay) counts() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.submits, a.polls
}

func newAsyncRelay(t *testing.T, pending int) (*asyncRelay, *httptest.Server) {
	a := &asyncRelay{pending: pending}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		a.mu.Lock()
		defer a.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "test-image"}}})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/images/generations"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			a.submits++
			a.idemKeys = append(a.idemKeys, r.Header.Get("Idempotency-Key"))
			a.async = append(a.async, body["async"] == true)
			w.WriteHeader(202)
			json.NewEncoder(w).Encode(map[string]any{"id": "job-1", "status": "queued"})
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/images/jobs/"):
			io.Copy(io.Discard, r.Body)
			a.polls++
			if a.polls <= a.pending {
				json.NewEncoder(w).Encode(map[string]any{"id": "job-1", "status": "running"})
				return
			}
			if a.fail {
				json.NewEncoder(w).Encode(map[string]any{"id": "job-1", "status": "failed", "error": map[string]string{"message": "content policy"}})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "job-1", "status": "succeeded", "data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG())}}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	return a, server
}

func fastPolling(t *testing.T) {
	every, timeout := asyncPollEvery, asyncPollTimeout
	asyncPollEvery = 10 * time.Millisecond
	t.Cleanup(func() { asyncPollEvery, asyncPollTimeout = every, timeout })
}

func asyncConnection(t *testing.T, s *Service, base string) Connection {
	t.Helper()
	c, e := s.SaveConnection(ConnectionInput{Name: "异步线路", BaseURL: base, Model: "test-image", Key: "async-key", Protocol: protocolAsyncJobs})
	if e != nil || c.Protocol != protocolAsyncJobs {
		t.Fatal(c, e)
	}
	return c
}

func onlyTask(t *testing.T, s *Service) Task {
	t.Helper()
	tasks, _ := List[Task](s.store, "task")
	if len(tasks) != 1 {
		t.Fatalf("%d tasks", len(tasks))
	}
	return tasks[0]
}

func TestAsyncJobIsSubmittedOnceAndPolled(t *testing.T) {
	fastPolling(t)
	relay, server := newAsyncRelay(t, 2)
	s := testService(t)
	c := asyncConnection(t, s, server.URL+"/v1")
	if _, e := s.Submit(request(c)); e != nil {
		t.Fatal(e)
	}
	for s.stepNext() {
	}
	task := onlyTask(t, s)
	submits, polls := relay.counts()
	if task.Status != "succeeded" || len(task.AssetIDs) != 1 || submits != 1 || polls != 3 {
		t.Fatalf("status %s assets %d submits %d polls %d err %s", task.Status, len(task.AssetIDs), submits, polls, task.Error)
	}
	if task.Diagnostic.UpstreamJobID != "job-1" || relay.idemKeys[0] != task.ID || !relay.async[0] {
		t.Fatalf("job %q idem %v async %v", task.Diagnostic.UpstreamJobID, relay.idemKeys, relay.async)
	}
}

func TestAsyncJobResumesAfterRestartWithoutResubmitting(t *testing.T) {
	fastPolling(t)
	relay, server := newAsyncRelay(t, 1)
	root := filepath.Join(t.TempDir(), "data")
	s, e := NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	c := asyncConnection(t, s, server.URL+"/v1")
	var full Connection
	s.store.Get("connection", c.ID, &full)
	// The app exited while job-1 was being generated.
	task := Task{ID: NewID(), BatchID: "b", CreatedAt: Now(), UpdatedAt: Now(), Status: "submitting", Prompt: "p", Model: "test-image", Size: "auto", Quality: "auto", Connection: full, References: []string{}, AssetIDs: []string{},
		Diagnostic: GenerationDiagnostic{Stage: "polling", UpstreamJobID: "job-1"}}
	s.store.Put("task", task.ID, task)
	s.Dispose()

	s, e = NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Dispose)
	if got := onlyTask(t, s); got.Status != "queued" {
		t.Fatalf("interrupted async task is %s, not resumed", got.Status)
	}
	for s.stepNext() {
	}
	got := onlyTask(t, s)
	submits, polls := relay.counts()
	if got.Status != "succeeded" || submits != 0 || polls != 2 {
		t.Fatalf("status %s submits %d polls %d err %s", got.Status, submits, polls, got.Error)
	}
}

func TestAsyncJobFailureAndTimeout(t *testing.T) {
	fastPolling(t)
	relay, server := newAsyncRelay(t, 0)
	relay.fail = true
	s := testService(t)
	c := asyncConnection(t, s, server.URL+"/v1")
	s.Submit(request(c))
	for s.stepNext() {
	}
	if got := onlyTask(t, s); got.Status != "failed" || !strings.Contains(got.Error, "content policy") {
		t.Fatalf("failed job: %s %s", got.Status, got.Error)
	}

	_, slow := newAsyncRelay(t, 1_000_000)
	asyncPollTimeout = 50 * time.Millisecond
	s2 := testService(t)
	c2 := asyncConnection(t, s2, slow.URL+"/v1")
	s2.Submit(request(c2))
	for s2.stepNext() {
	}
	if got := onlyTask(t, s2); got.Status != "unknown" || !strings.Contains(got.Error, "job-1") {
		t.Fatalf("timed-out job: %s %s", got.Status, got.Error)
	}
}

func TestImageProtocolIsValidated(t *testing.T) {
	s := testService(t)
	if _, e := s.SaveConnection(ConnectionInput{Name: "x", BaseURL: "http://127.0.0.1:1/v1", Model: "m", Key: "k", Protocol: "vendor-x"}); e == nil {
		t.Fatal("unknown protocol saved")
	}
	c, e := s.SaveConnection(ConnectionInput{Name: "x", BaseURL: "http://127.0.0.1:1/v1", Model: "m", Key: "k"})
	if e != nil || c.Protocol != protocolOpenAIImages {
		t.Fatal(c, e)
	}
}
