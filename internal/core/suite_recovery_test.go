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

// suiteTask is one slot of a 套图 as it is stored after a batch went wrong.
func suiteTask(c Connection, runID, slot, status string) Task {
	return Task{ID: NewID(), BatchID: "batch-" + runID, AgentRunID: runID, PlanItemID: slot, CreatedAt: Now(), UpdatedAt: Now(), Status: status, Prompt: "p", Model: "m", Size: "1024x1024", Connection: c, References: []string{}, AssetIDs: []string{}}
}

// One confirmed click completes the whole set: failed, paused and 结果未知
// images are all queued again.
func TestBulkRetryIncludesUnknownResults(t *testing.T) {
	s := testService(t)
	var c Connection
	s.store.Get("connection", saveConnection(t, s, "http://127.0.0.1:9").ID, &c)
	runID := NewID()
	tasks := []Task{}
	for i, status := range []string{"succeeded", "failed", "blocked", "unknown", "unknown"} {
		tasks = append(tasks, suiteTask(c, runID, fmt.Sprintf("slot-%d", i+1), status))
	}
	if _, e := s.store.Submit("suite", "batch-"+runID, tasks); e != nil {
		t.Fatal(e)
	}
	n, e := s.RetryFailedAgentImages(runID)
	if e != nil || n != 4 {
		t.Fatal("bulk retry count", n, e)
	}
	current, _ := s.GetAgentTasks(runID)
	for _, task := range current {
		want := "queued"
		if task.PlanItemID == "slot-1" {
			want = "succeeded"
		}
		if task.Status != want {
			t.Fatal("slot not queued again", task.PlanItemID, task.Status)
		}
	}
}

// relay answers every image request with status and body until ok is set,
// then with an image.
func relay(t *testing.T, status int, body string) (*httptest.Server, *atomic.Bool, *atomic.Int32) {
	var ok atomic.Bool
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if ok.Load() {
			json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG())}}})
			return
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, &ok, &requests
}

func storedConnection(t *testing.T, s *Service, base string) Connection {
	var c Connection
	s.store.Get("connection", saveConnection(t, s, base).ID, &c) // keeps the encrypted key reference
	return c
}

func refusalCode(t Task) string {
	b, _ := json.Marshal(t)
	var got struct {
		Refusal string `json:"refusal"`
	}
	json.Unmarshal(b, &got)
	return got.Refusal
}

// A relay that refuses because the quota or balance is used up says so in the
// message, whatever the status; the customer reads 线路额度已用完 and the rest
// of the set pauses unsent.
func TestQuotaRefusalSaysLineQuotaUsedUpAndPausesTheSet(t *testing.T) {
	for _, status := range []int{401, 402, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server, _, requests := relay(t, status, `{"error":{"message":"该令牌额度已用尽 !token.UnlimitedQuota && token.RemainQuota = 0"}}`)
			s := testService(t)
			c := storedConnection(t, s, server.URL)
			runID := NewID()
			tasks := []Task{}
			for i := 1; i <= 3; i++ {
				tasks = append(tasks, suiteTask(c, runID, fmt.Sprintf("slot-%d", i), "queued"))
			}
			if _, e := s.store.Submit("suite", "batch-"+runID, tasks); e != nil {
				t.Fatal(e)
			}
			for s.stepNext() {
			}
			if requests.Load() != 1 {
				t.Fatal("images after the refusal were still sent", requests.Load())
			}
			current, _ := s.GetAgentTasks(runID)
			for _, task := range current {
				switch task.Status {
				case "failed":
					if !strings.HasPrefix(task.Error, "线路额度已用完") || refusalCode(task) != "balance" {
						t.Fatal("quota refusal not named", task.Error, refusalCode(task))
					}
				case "blocked":
					if !strings.Contains(task.Error, "线路额度已用完") || !strings.Contains(task.Error, "尚未提交、未计费") {
						t.Fatal("pause reason", task.Error)
					}
				default:
					t.Fatal("unexpected status", task.Status)
				}
			}
			if countStatus(s, "blocked") != 2 {
				t.Fatal("the rest of the set did not pause")
			}
		})
	}
}

// A 结果未知 image whose automatic resend is refused for a used-up quota shows
// that refusal, not a second unknown result. Relays answer it with a 4xx, and
// some wrap it in a 5xx.
func TestAutomaticResendRefusedForQuotaShowsTheRefusal(t *testing.T) {
	for _, status := range []int{401, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				if requests.Add(1) == 1 {
					w.WriteHeader(524)
					io.WriteString(w, `{"error":{"message":"error code: 524"}}`)
					return
				}
				w.WriteHeader(status)
				io.WriteString(w, `{"error":{"message":"该令牌额度已用尽 !token.UnlimitedQuota && token.RemainQuota = 0"}}`)
			}))
			defer server.Close()
			s := testService(t)
			c := storedConnection(t, s, server.URL)
			runID := NewID()
			if _, e := s.store.Submit("suite", "batch-"+runID, []Task{suiteTask(c, runID, "slot-1", "queued")}); e != nil {
				t.Fatal(e)
			}
			for s.stepNext() {
			}
			if requests.Load() != 2 {
				t.Fatal("requests", requests.Load())
			}
			current, _ := s.GetAgentTasks(runID)
			got := current[0]
			if got.AutoRetryOf == "" || got.Status != "failed" || refusalCode(got) != "balance" || !strings.HasPrefix(got.Error, "线路额度已用完") {
				t.Fatal("the refusal of the automatic resend is not shown", got.Status, refusalCode(got), got.Error)
			}
			if strings.Contains(got.Error, "结果未知") || strings.Contains(got.Error, "不会再次自动重发") {
				t.Fatal("shown as unknown", got.Error)
			}
		})
	}
}

// An image is resent automatically at most once; the customer's one-click
// completion is an explicit action and resends it again.
func TestAutomaticResendOnceButBulkRetryResendsAgain(t *testing.T) {
	server, ok, requests := relay(t, 524, `{"error":{"message":"error code: 524"}}`)
	s := testService(t)
	c := storedConnection(t, s, server.URL)
	runID := NewID()
	if _, e := s.store.Submit("suite", "batch-"+runID, []Task{suiteTask(c, runID, "slot-1", "queued")}); e != nil {
		t.Fatal(e)
	}
	for s.stepNext() {
	}
	if requests.Load() != 2 {
		t.Fatal("a 结果未知 image is resent automatically exactly once", requests.Load())
	}
	all, _ := List[Task](s.store, "task")
	if len(all) != 2 || all[0].Status != "unknown" || all[0].AutoRetryOf != all[1].ID || all[0].AutoRetriedAs != "" {
		t.Fatal("automatic resend chain", all)
	}
	ok.Store(true)
	if n, e := s.RetryFailedAgentImages(runID); e != nil || n != 1 {
		t.Fatal("bulk retry resends the image the app would not resend", n, e)
	}
	for s.stepNext() {
	}
	current, _ := s.GetAgentTasks(runID)
	if requests.Load() != 3 || len(current) != 1 || current[0].Status != "succeeded" || current[0].AutoRetryOf != "" {
		t.Fatal("bulk retry", requests.Load(), current)
	}
}
