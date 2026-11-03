package core

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const refusal = `{"error":{"message":"非常抱歉，该提示可能违反了我们的内容政策。test-key","code":"moderation_blocked"}}`

// refusingRelay refuses the first n image requests with a content-policy 400.
func refusingRelay(t *testing.T, n int32, prompts *[]string) (*httptest.Server, *atomic.Int32) {
	var hits atomic.Int32
	var mu sync.Mutex
	png := base64.StdEncoding.EncodeToString(tinyPNG())
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		*prompts = append(*prompts, m["prompt"].(string))
		mu.Unlock()
		if hits.Add(1) <= n {
			w.WriteHeader(400)
			io.WriteString(w, refusal)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []Output{{Base64: png}}})
	}))
	t.Cleanup(up.Close)
	return up, &hits
}

func rewordingLLM(t *testing.T, s *Service, reply string) *atomic.Int32 {
	var hits atomic.Int32
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"content": []map[string]any{{"type": "text", "text": reply}}, "usage": map[string]any{"input_tokens": 300, "output_tokens": 80}})
	}))
	t.Cleanup(llm.Close)
	if _, e := s.SaveConnection(ConnectionInput{Kind: "llm", Name: "LLM 线路", BaseURL: llm.URL, Model: "test-llm", Key: "llm-key"}); e != nil {
		t.Fatal(e)
	}
	return &hits
}

// waitReworded waits until no attempt is held for rewording.
func waitReworded(t *testing.T, s *Service) []Task {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		s.mu.Lock()
		tasks, _ := List[Task](s.store, "task")
		busy := len(s.rewording) > 0
		s.mu.Unlock()
		held := false
		for _, x := range tasks {
			held = held || (x.Rewording && x.Status == "queued")
		}
		if !held && !busy {
			return tasks
		}
	}
	t.Fatal("prompt was not reworded")
	return nil
}

func TestPolicyRejected(t *testing.T) {
	for _, c := range []struct {
		status int
		msg    string
		want   bool
	}{
		{400, "HTTP 400：非常抱歉，该提示可能违反了我们的内容政策。如果你认为此判断有误，请重试或修改提示语。", true},
		{400, "Your request was rejected as a result of our safety system.", true},
		{400, "moderation_blocked", true},
		{403, "content_policy_violation", true},
		{400, "Invalid size 9999x9999", false},
		{401, "safety system", false},
		{429, "moderation rate limit", false},
		{503, "content policy", false},
	} {
		if got := policyRejected(c.status, c.msg); got != c.want {
			t.Errorf("policyRejected(%d, %q) = %v", c.status, c.msg, got)
		}
	}
}

func TestPolicyRefusalResendsThenRewords(t *testing.T) {
	s := testService(t)
	var prompts []string
	up, hits := refusingRelay(t, 2, &prompts)
	llmHits := rewordingLLM(t, s, "画一只金毛犬趴在地毯上，旁边是绣着 \"Bella\" 的圣诞袜")
	c := saveConnection(t, s, up.URL)
	in := request(c)
	in.Prompts = []string{"画一只金毛犬躺在地毯上，旁边是绣着 \"Bella\" 的圣诞袜"}
	s.Submit(in)
	s.stepNext()
	p, _ := s.ListTasks("", 0, 10)
	first := p.Items[0]
	if len(p.Items) != 2 || p.Items[1].Status != "failed" || p.Items[1].PolicyRetriedAs != first.ID || first.PolicyRetryOf != p.Items[1].ID || first.PolicyStep != 1 || first.Rewording {
		t.Fatalf("refused image not resent unchanged: %+v", p.Items)
	}
	if e := p.Items[1].Error; !strings.HasPrefix(e, policyRefusedPrefix) || !strings.Contains(e, "原样重发") || strings.Contains(e, "test-key") {
		t.Fatal(e)
	}
	s.stepNext()
	p, _ = s.ListTasks("", 0, 10)
	if len(p.Items) != 3 || p.Items[0].PolicyStep != 2 || p.Items[1].PolicyRetriedAs != p.Items[0].ID || !strings.Contains(p.Items[1].Error, "改写") {
		t.Fatalf("second refusal not reworded: %+v", p.Items)
	}
	waitReworded(t, s)
	s.stepNext()
	p, _ = s.ListTasks("", 0, 10)
	if p.Items[0].Status != "succeeded" || hits.Load() != 3 || llmHits.Load() != 1 {
		t.Fatal(p.Items[0].Status, p.Items[0].Error, hits.Load(), llmHits.Load())
	}
	if prompts[0] != prompts[1] || !strings.Contains(prompts[2], "趴在地毯上") {
		t.Fatal("resend changed or reword not used", prompts)
	}
	calls, _ := List[LLMCall](s.store, "llm_call")
	if len(calls) != 1 || calls[0].Kind != "rewrite" || calls[0].Status != "completed" || calls[0].OutputTokens != 80 {
		t.Fatalf("rewrite call not recorded: %+v", calls)
	}
	// A content refusal is about the image, not the gateway.
	tasks, _ := List[Task](s.store, "task")
	for _, x := range tasks {
		if o, ok := taskOutcome(x); ok && o.rejected != "" {
			t.Fatal("refusal counted against the gateway")
		}
	}
}

func TestPolicyRefusalStopsAfterRewording(t *testing.T) {
	s := testService(t)
	var prompts []string
	up, hits := refusingRelay(t, 99, &prompts)
	rewordingLLM(t, s, "改写后的 \"Bella\" 圣诞袜")
	c := saveConnection(t, s, up.URL)
	in := request(c)
	in.Prompts = []string{"\"Bella\" 圣诞袜"}
	s.Submit(in)
	s.stepNext()
	s.stepNext()
	waitReworded(t, s)
	s.stepNext()
	s.stepNext()
	p, _ := s.ListTasks("", 0, 10)
	if len(p.Items) != 3 || hits.Load() != 3 || p.Items[0].Status != "failed" || !strings.Contains(p.Items[0].Error, "不再自动重发") {
		t.Fatal(len(p.Items), hits.Load(), p.Items[0].Error)
	}
	// A manual retry starts the automatic resends afresh.
	if _, e := s.Retry(p.Items[0].ID); e != nil {
		t.Fatal(e)
	}
}

func TestPolicyRewordKeepsQuotedTextOrFails(t *testing.T) {
	s := testService(t)
	var prompts []string
	up, hits := refusingRelay(t, 2, &prompts)
	rewordingLLM(t, s, "一只狗和圣诞袜，袜子上绣着名字")
	c := saveConnection(t, s, up.URL)
	in := request(c)
	in.Prompts = []string{"一只狗和圣诞袜，袜子上绣着“Bella”"}
	s.Submit(in)
	s.stepNext()
	s.stepNext()
	tasks := waitReworded(t, s)
	if tasks[0].Status != "failed" || !strings.Contains(tasks[0].Error, "没有保留画面文字“Bella”") || !strings.Contains(tasks[0].Error, "不计费") || hits.Load() != 2 {
		t.Fatal(tasks[0].Status, tasks[0].Error, hits.Load())
	}
}

func TestPolicyRefusalWithoutLLMStopsAfterResend(t *testing.T) {
	s := testService(t)
	var prompts []string
	up, hits := refusingRelay(t, 99, &prompts)
	c := saveConnection(t, s, up.URL)
	s.Submit(request(c))
	s.stepNext()
	s.stepNext()
	s.stepNext()
	p, _ := s.ListTasks("", 0, 10)
	if len(p.Items) != 2 || hits.Load() != 2 || !strings.Contains(p.Items[0].Error, "没有可用的 LLM") {
		t.Fatal(len(p.Items), hits.Load(), p.Items[0].Error)
	}
}

func TestHeldRewordResumesOnStart(t *testing.T) {
	s := testService(t)
	c := saveConnection(t, s, "http://127.0.0.1:1/v1")
	held := Task{ID: NewID(), BatchID: "b", CreatedAt: Now(), UpdatedAt: Now(), Status: "queued", Prompt: "p", Model: "m", Connection: c, References: []string{}, AssetIDs: []string{}, PolicyStep: 2, Rewording: true}
	s.store.Put("task", held.ID, held)
	if s.step(held) {
		t.Fatal("a held attempt was sent before its prompt was reworded")
	}
	// Without an LLM line the held attempt fails unsent instead of waiting forever.
	if s.claimTask(false) {
		t.Fatal("a held attempt was claimed for sending")
	}
	tasks := waitReworded(t, s)
	if tasks[0].Status != "failed" || !strings.Contains(tasks[0].Error, "没有发送、不计费") {
		t.Fatal(tasks[0].Status, tasks[0].Error)
	}
}
