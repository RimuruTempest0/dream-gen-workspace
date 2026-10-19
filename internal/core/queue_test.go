package core

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// slowRelay holds every generation request until released and records the
// highest number of requests that were open at the same time.
type slowRelay struct {
	open, peak, total atomic.Int32
	release           chan struct{}
}

func newSlowRelay(t *testing.T) (*slowRelay, *httptest.Server) {
	r := &slowRelay{release: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		io.Copy(io.Discard, req.Body)
		r.total.Add(1)
		now := r.open.Add(1)
		for {
			peak := r.peak.Load()
			if now <= peak || r.peak.CompareAndSwap(peak, now) {
				break
			}
		}
		<-r.release
		r.open.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG())}}})
	}))
	t.Cleanup(server.Close)
	return r, server
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for", what)
}

func countStatus(s *Service, status string) int {
	tasks, _ := List[Task](s.store, "task")
	n := 0
	for _, t := range tasks {
		if t.Status == status {
			n++
		}
	}
	return n
}

func TestQueueRunsConfiguredNumberInParallel(t *testing.T) {
	relay, server := newSlowRelay(t)
	s := testService(t)
	if s.ImageConcurrency() != defaultImageConcurrency {
		t.Fatal("default concurrency", s.ImageConcurrency())
	}
	c := saveConnection(t, s, server.URL)
	if _, e := s.Submit(GenerateInput{RequestID: "parallel", ConnectionID: c.ID, Model: "m", Prompts: []string{"a", "b", "c", "d", "e", "f", "g"}, Count: 1, References: []string{}, Size: "auto", Quality: "auto"}); e != nil {
		t.Fatal(e)
	}
	s.Start(func() {})
	waitFor(t, "three open requests", func() bool { return relay.open.Load() == 3 })
	time.Sleep(200 * time.Millisecond)
	if relay.peak.Load() != 3 || countStatus(s, "queued") != 4 {
		t.Fatal("expected exactly three in flight", relay.peak.Load(), countStatus(s, "queued"))
	}

	// Raising the limit starts more immediately; lowering it starts nothing new.
	if e := s.SetImageConcurrency(5); e != nil {
		t.Fatal(e)
	}
	waitFor(t, "five open requests", func() bool { return relay.open.Load() == 5 })
	if e := s.SetImageConcurrency(1); e != nil {
		t.Fatal(e)
	}
	close(relay.release)
	waitFor(t, "all succeeded", func() bool { return countStatus(s, "succeeded") == 7 })
	if relay.total.Load() != 7 || relay.peak.Load() != 5 {
		t.Fatal("each task must be submitted exactly once", relay.total.Load(), relay.peak.Load())
	}
	for _, bad := range []int{0, 9} {
		if s.SetImageConcurrency(bad) == nil {
			t.Fatal("out of range concurrency accepted", bad)
		}
	}
}

func TestStoppedTaskIsNotSubmittedAfterClaim(t *testing.T) {
	relay, server := newSlowRelay(t)
	close(relay.release)
	s := testService(t)
	c := saveConnection(t, s, server.URL)
	batch, e := s.Submit(GenerateInput{RequestID: "stop", ConnectionID: c.ID, Model: "m", Prompts: []string{"a"}, Count: 1, References: []string{}, Size: "auto", Quality: "auto"})
	if e != nil {
		t.Fatal(e)
	}
	tasks, _ := List[Task](s.store, "task")
	claimed := tasks[0]
	if e = s.StopBatch(batch); e != nil {
		t.Fatal(e)
	}
	if s.step(claimed) || relay.total.Load() != 0 {
		t.Fatal("a task stopped after being claimed was still sent")
	}
}

func TestConcurrencyPreferencePersists(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	s, e := NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetImageConcurrency(6); e != nil {
		t.Fatal(e)
	}
	s.Dispose()
	next, e := NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Dispose()
	if next.ImageConcurrency() != 6 {
		t.Fatal("concurrency preference lost", next.ImageConcurrency())
	}
}
