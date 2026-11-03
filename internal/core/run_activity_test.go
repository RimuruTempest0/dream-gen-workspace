package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// silentLLM never answers a Messages request until the test ends.
func silentLLM(t *testing.T, hits *atomic.Int32) *httptest.Server {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		hits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})
	return server
}

// answeringLLM streams one short reply and records the model it was asked for.
func answeringLLM(t *testing.T, hits *atomic.Int32, model *atomic.Value) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		model.Store(body.Model)
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"好\"}}\n\n")
	}))
	t.Cleanup(server.Close)
	return server
}

func shortFirstReply(t *testing.T) {
	t.Helper()
	old := llmFirstReplyTimeout
	llmFirstReplyTimeout = 150 * time.Millisecond
	t.Cleanup(func() { llmFirstReplyTimeout = old })
}

func managedLLM(t *testing.T, s *Service, slot, base, model string) Connection {
	t.Helper()
	c := saveLLM(t, s, base)
	var full Connection
	if e := s.store.Get("connection", c.ID, &full); e != nil {
		t.Fatal(e)
	}
	full.Managed, full.Model, full.Name = slot, model, slot
	full.Capabilities.Vision = true
	if e := s.store.Put("connection", full.ID, full); e != nil {
		t.Fatal(e)
	}
	return normalizeConnection(full)
}

func bridgeFor(t *testing.T, s *Service, main Connection) (*agentBridge, string) {
	t.Helper()
	r := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "analyzing", Progress: newRunProgress(), LLMConnection: main}
	if e := s.store.Put("agent_run", r.ID, r); e != nil {
		t.Fatal(e)
	}
	return &agentBridge{service: s, runID: r.ID, token: "t", key: "k", connection: main, startID: main.ID, client: llmHTTPClient(), ctx: context.Background()}, r.ID
}

func sendMessages(b *agentBridge, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()
	b.serve(rec, req)
	return rec
}

const streamRequest = `{"model":"main-model","stream":true,"messages":[{"role":"user","content":"秘密提示词"}]}`

func runOf(t *testing.T, s *Service, id string) AgentRun {
	t.Helper()
	var r AgentRun
	if e := s.store.Get("agent_run", id, &r); e != nil {
		t.Fatal(e)
	}
	return r
}

func feedText(r AgentRun) string {
	var b strings.Builder
	for _, a := range r.Activity {
		b.WriteString(a.Kind + ":" + a.Text + "\n")
	}
	return b.String()
}

func TestSilentRequestMovesToTheBackupLine(t *testing.T) {
	shortFirstReply(t)
	var silent, answered atomic.Int32
	var model atomic.Value
	main := silentLLM(t, &silent)
	backup := answeringLLM(t, &answered, &model)
	s := testService(t)
	line1 := managedLLM(t, s, "llm-1", main.URL, "main-model")
	managedLLM(t, s, "llm-2", backup.URL, "backup-model")
	b, id := bridgeFor(t, s, line1)
	rec := sendMessages(b, streamRequest)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "好") {
		t.Fatalf("reply did not come from the backup: %d %s", rec.Code, rec.Body.String())
	}
	if silent.Load() != 1 || answered.Load() != 1 || model.Load() != "backup-model" {
		t.Fatal(silent.Load(), answered.Load(), model.Load())
	}
	// The rest of the attempt stays on the backup without waiting again.
	started := time.Now()
	if rec = sendMessages(b, streamRequest); rec.Code != 200 || silent.Load() != 1 || answered.Load() != 2 || time.Since(started) > 120*time.Millisecond {
		t.Fatal("next request did not go straight to the backup", rec.Code, silent.Load(), answered.Load())
	}
	r := runOf(t, s, id)
	feed := feedText(r)
	if !strings.Contains(r.Progress.LineNote, "已改用备用线路") || !strings.Contains(feed, "已改用备用线路") || !strings.Contains(feed, "没有成功") {
		t.Fatalf("%q\n%s", r.Progress.LineNote, feed)
	}
	if strings.Contains(feed, "秘密提示词") || strings.Contains(rec.Body.String(), "k\"") {
		t.Fatal("prompt or key in the feed")
	}
}

func TestTrialStopsWithAReasonInsteadOfUsingTheBackup(t *testing.T) {
	shortFirstReply(t)
	var silent, answered atomic.Int32
	var model atomic.Value
	main := silentLLM(t, &silent)
	backup := answeringLLM(t, &answered, &model)
	s := testService(t)
	line1 := managedLLM(t, s, "llm-1", main.URL, "main-model")
	managedLLM(t, s, "llm-2", backup.URL, "backup-model")
	s.lic.mode = "trial"
	b, id := bridgeFor(t, s, line1)
	rec := sendMessages(b, streamRequest)
	if rec.Code != 529 || silent.Load() != 2 || answered.Load() != 0 {
		t.Fatal("expected one retry on the main line, then a stop", rec.Code, silent.Load(), answered.Load())
	}
	r := runOf(t, s, id)
	if r.StopReason != noReplyStopped || interruptedMessage(r) != noReplyStopped || !strings.Contains(r.Progress.LineNote, "正在重试") {
		t.Fatalf("%+v", r.StopReason)
	}
}

func TestSlowButLiveRepliesAreLeftAlone(t *testing.T) {
	shortFirstReply(t)
	// A reply that began in time may pause longer than the limit.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"delta\":{\"text\":\"一\"}}\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(300 * time.Millisecond)
		io.WriteString(w, "data: {\"delta\":{\"text\":\"二\"}}\n\n")
	}))
	defer server.Close()
	s := testService(t)
	b, _ := bridgeFor(t, s, managedLLM(t, s, "llm-1", server.URL, "m"))
	if rec := sendMessages(b, streamRequest); rec.Code != 200 || !strings.Contains(rec.Body.String(), "二") {
		t.Fatal(rec.Code, rec.Body.String())
	}
	// A non-streaming reply says nothing until it is complete, so it has no limit.
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"type":"message","content":[]}`)
	}))
	defer plain.Close()
	b2, _ := bridgeFor(t, s, managedLLM(t, s, "llm-3", plain.URL, "m"))
	if rec := sendMessages(b2, `{"model":"m","messages":[]}`); rec.Code != 200 {
		t.Fatal("non-streaming request was cut", rec.Code)
	}
}

func TestInterruptedMessageOnlyMentionsAPlanThatExists(t *testing.T) {
	cases := []struct {
		run  AgentRun
		want string
		not  string
	}{
		{AgentRun{}, "资料已保存", "方案"},
		{AgentRun{Plans: []SuitePlan{{}}}, "资料和方案已保存", ""},
		{AgentRun{CancelRequested: true}, "已取消", "20 分钟"},
		{AgentRun{StopReason: noReplyStopped}, "模型一直没有回复", "20 分钟"},
	}
	for _, c := range cases {
		got := interruptedMessage(c.run)
		if !strings.Contains(got, c.want) || (c.not != "" && strings.Contains(got, c.not)) {
			t.Errorf("%q", got)
		}
	}
}

func TestContinuedAttemptShowsEarlierStepsAsDone(t *testing.T) {
	p := progressForAttempt(AgentRun{Product: ProductInfo{SourceURL: "https://x", ReferenceIDs: []string{"a", "b"}}})
	if p.Step != stepPrepare || !strings.Contains(strings.Join(p.Visited, ","), stepImages) || p.ImagesSaved != 2 {
		t.Fatalf("%+v", p)
	}
	if p = progressForAttempt(AgentRun{Product: ProductInfo{SourceURL: "https://x", Error: "读不到"}}); strings.Contains(strings.Join(p.Visited, ","), stepProduct) {
		t.Fatal("an unread page shown as done")
	}
}

func TestActivityFeedNamesActionsNotContent(t *testing.T) {
	s := testService(t)
	r := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "analyzing", Progress: newRunProgress()}
	s.store.Put("agent_run", r.ID, r)
	clean := func(text string) string { return redact(text, "sk-secret") }
	plan, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"content": []map[string]any{
		{"type": "text", "text": "现在保存方案 sk-secret"},
		{"type": "tool_use", "name": "mcp__workbench__save_suite_plan", "input": map[string]any{"append": true, "locked": []string{"秘密锁定项"}, "items": []map[string]string{{"prompt": "秘密提示词"}, {"prompt": "第二条"}}}},
		{"type": "tool_use", "name": "Skill", "input": map[string]any{"skill": "p004-rules"}},
	}}})
	s.noteAgentEvent(r.ID, plan, clean)
	s.noteAgentEvent(r.ID, []byte(`{"type":"system","subtype":"init","claude_code_version":"2.1.296","model":"m","tools":["a","b"]}`), clean)
	s.noteAgentEvent(r.ID, []byte(`{"type":"result","num_turns":7,"duration_ms":61000}`), clean)
	saved := runOf(t, s, r.ID)
	feed := feedText(saved)
	for _, want := range []string{"暂存套图方案：本批 2 张", "加载技能：P004 生图规则", "Claude Code 2.1.296 已启动", "7 个回合", "1 分 1 秒"} {
		if !strings.Contains(feed, want) {
			t.Errorf("missing %q in\n%s", want, feed)
		}
	}
	for _, not := range []string{"秘密", "第二条", "sk-secret"} {
		if strings.Contains(feed, not) {
			t.Errorf("%q leaked into\n%s", not, feed)
		}
	}
	if saved.Progress.AgentVersion != "2.1.296" {
		t.Fatal(saved.Progress.AgentVersion)
	}
	long := strings.Repeat("字", maxActivityText+50)
	s.addActivity(r.ID, "agent", long)
	for i := 0; i < maxActivityEntries+5; i++ {
		s.addActivity(r.ID, "tool", "x")
	}
	if n := len(runOf(t, s, r.ID).Activity); n != maxActivityEntries {
		t.Fatal("feed not capped", n)
	}
}

// A reply that spends the whole limit on reasoning turns reasoning off for the rest of
// the attempt; a provider that refuses the setting gets the request again without it.
func TestReasoningOverflowTurnsThinkingOff(t *testing.T) {
	for _, refuses := range []bool{false, true} {
		var requests []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			requests = append(requests, string(raw))
			off := strings.Contains(string(raw), "\"thinking\":{\"type\":\"disabled\"}")
			if off && refuses {
				w.WriteHeader(400)
				io.WriteString(w, `{"error":{"message":"unknown thinking"}}`)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			if len(requests) == 1 {
				// Reasoning, then a tool call cut off by the limit.
				io.WriteString(w, "data: {\"delta\":{\"thinking\":\"想很久\"}}\n\ndata: {\"content_block\":{\"type\":\"tool_use\",\"name\":\"mcp__workbench__save_suite_plan\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
				return
			}
			io.WriteString(w, "data: {\"content_block\":{\"type\":\"text\"},\"delta\":{\"text\":\"写好了\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
		}))
		s := testService(t)
		line := managedLLM(t, s, "llm-1", server.URL, "m")
		line.Mode = "compatible"
		b, id := bridgeFor(t, s, line)
		if rec := sendMessages(b, streamRequest); rec.Code != 200 {
			t.Fatal(rec.Code)
		}
		if strings.Contains(requests[0], "disabled") {
			t.Fatal("first request should keep the model's default")
		}
		rec := sendMessages(b, streamRequest)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "写好了") {
			t.Fatal(refuses, rec.Code, rec.Body.String())
		}
		if !strings.Contains(requests[1], "disabled") {
			t.Fatal("reasoning not turned off", requests[1])
		}
		if refuses && (len(requests) != 3 || strings.Contains(requests[2], "disabled")) {
			t.Fatal("refused setting was not dropped", len(requests))
		}
		// A refusal is remembered for the rest of the attempt.
		if refuses {
			sendMessages(b, streamRequest)
			if len(requests) != 4 || strings.Contains(requests[3], "disabled") {
				t.Fatal("refused setting sent again")
			}
		}
		if feed := feedText(runOf(t, s, id)); !strings.Contains(feed, "关闭思考") {
			t.Fatal(feed)
		}
		server.Close()
	}
}

func TestFailureReasonsAreInPlainWords(t *testing.T) {
	for message, want := range map[string]string{
		`Post "https://x": net/http: TLS handshake timeout`: "网络握手超时",
		"unexpected EOF":                        "中途切断",
		"context deadline exceeded":             "时间上限",
		"no reply within the first-reply limit": "一直没有回复",
	} {
		if !strings.Contains(llmFailureReason(message), want) {
			t.Errorf("%q: %q", message, llmFailureReason(message))
		}
	}
	clean := func(v string) string { return strings.ReplaceAll(v, "sk-1", "[隐藏]") }
	if got := toolErrorText(json.RawMessage(`"<tool_use_error>InputValidationError: x was called with input that could not be parsed as JSON."`), clean); !strings.Contains(got, "截断") {
		t.Fatal(got)
	}
	if got := toolErrorText(json.RawMessage(`"The operation timed out."`), clean); !strings.Contains(got, "超时") {
		t.Fatal(got)
	}
	if got := toolErrorText(json.RawMessage(`[{"type":"text","text":"坏了 sk-1"}]`), clean); !strings.Contains(got, "坏了 [隐藏]") {
		t.Fatal(got)
	}
}

// The page is read before the Agent starts, and the tool then returns it at once.
func TestProductIsReadBeforeTheAgentAndNotAgain(t *testing.T) {
	var pages atomic.Int32
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages.Add(1)
		io.WriteString(w, `<html><head><title>好商品</title></head><body>这是一段足够长的商品描述，用来确认页面已经读取，内容包含材质和尺寸等信息。</body></html>`)
	}))
	defer shop.Close()
	s := testService(t)
	s.allowLocalProduct = true
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	input := agentInput(llm, images)
	input.ProductURL = shop.URL + "/item"
	var atStart int32
	var title string
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		atStart = pages.Load()
		if !strings.Contains(in.Prompt, "好商品") {
			t.Error("the page read before the Agent started is not in its prompt")
		}
		id := invocationRun(t, in).ID
		b := &agentBridge{service: s, runID: id, ctx: context.Background()}
		out, e := b.callTool("fetch_product", nil)
		if e != nil {
			return e
		}
		title = out[0].(map[string]any)["text"].(string)
		emitAgentSuccess(in)
		return nil
	}
	if _, e := s.StartSmartGeneration(input, "product-first"); e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	if atStart != 1 || pages.Load() != 1 || !strings.Contains(title, "好商品") {
		t.Fatal(atStart, pages.Load(), title)
	}
}
