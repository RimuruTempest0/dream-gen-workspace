package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func taskByStatus(s *Service, status string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, _ := List[Task](s.store, "task")
	for _, t := range tasks {
		if t.Status == status {
			return t, true
		}
	}
	return Task{}, false
}

func TestCancelUnsentAndInFlightImages(t *testing.T) {
	relay, server := newSlowRelay(t)
	defer close(relay.release)
	s := testService(t)
	if e := s.SetImageConcurrency(1); e != nil {
		t.Fatal(e)
	}
	c := saveConnection(t, s, server.URL)
	if _, e := s.Submit(GenerateInput{RequestID: "cancel", ConnectionID: c.ID, Model: "m", Prompts: []string{"a", "b"}, Count: 1, References: []string{}, Size: "auto", Quality: "auto"}); e != nil {
		t.Fatal(e)
	}
	s.Start(func() {})
	waitFor(t, "one open request", func() bool { return relay.open.Load() == 1 })
	waitFor(t, "a submitting image", func() bool { _, ok := taskByStatus(s, "submitting"); return ok })

	// The waiting image is dropped without being sent.
	queued, _ := taskByStatus(s, "queued")
	if e := s.CancelTask(queued.ID); e != nil {
		t.Fatal(e)
	}
	// The image waiting on the relay stops waiting.
	sent, _ := taskByStatus(s, "submitting")
	if e := s.CancelTask(sent.ID); e != nil {
		t.Fatal(e)
	}
	waitFor(t, "both cancelled", func() bool { return countStatus(s, "canceled") == 2 })
	var a, b Task
	s.store.Get("task", queued.ID, &a)
	s.store.Get("task", sent.ID, &b)
	if a.Error != canceledUnsent || b.Error != canceledInFlight {
		t.Fatalf("messages: %q / %q", a.Error, b.Error)
	}
	if relay.total.Load() != 1 {
		t.Fatal("the cancelled waiting image was sent", relay.total.Load())
	}
	if s.CancelTask(sent.ID) == nil {
		t.Fatal("a finished image was cancelled again")
	}

	// A result that already came back is kept.
	b.Status = "downloading"
	s.store.Put("task", b.ID, b)
	if s.CancelTask(b.ID) == nil {
		t.Fatal("a returned image was cancelled")
	}
}

func TestModelListFallsBackToSavedList(t *testing.T) {
	var calls atomic.Int32
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if failing.Load() {
			w.WriteHeader(502)
			return
		}
		io.WriteString(w, `{"data":[{"id":"gpt-image-2.5"},{"id":"gpt-image-2"},{"id":"gpt-image-2-w"}]}`)
	}))
	defer server.Close()
	s := testService(t)
	c := saveConnection(t, s, server.URL)
	want := []string{"gpt-image-2", "gpt-image-2-w", "gpt-image-2.5"}
	if got, e := s.ListConnectionModels(c.ID); e != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, e)
	}
	// The relay fails later: the last list is shown instead of nothing.
	failing.Store(true)
	if got, e := s.ListConnectionModels(c.ID); e != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("no fallback", got, e)
	}
	if e := s.DeleteConnection(c.ID); e != nil {
		t.Fatal(e)
	}
	var left []string
	if s.store.Get(modelListKind, c.ID, &left) == nil {
		t.Fatal("model list kept after the connection was deleted")
	}

	// A connection test of the saved connection also remembers the list.
	failing.Store(false)
	c = saveConnection(t, s, server.URL)
	if r, e := s.TestConnection(ConnectionInput{ID: c.ID, Kind: "image", BaseURL: server.URL}); e != nil || len(r.IDs) != 3 {
		t.Fatal(r, e)
	}
	failing.Store(true)
	if got, e := s.ListConnectionModels(c.ID); e != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("test result not remembered", got, e)
	}
	// Without any saved list the error is reported.
	other := saveConnection(t, s, server.URL+"/other")
	if _, e := s.ListConnectionModels(other.ID); e == nil {
		t.Fatal("expected an error without a saved list")
	}
}
