package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGatewayMonitorCountsEachSentImageOnItsGateway(t *testing.T) {
	s := testService(t)
	primary := &fakeGateway{models: []string{"test-image"}, timeout: true}
	backup := &fakeGateway{models: []string{"test-image"}}
	a := gatewayConnection(t, s, "A", newFakeGateway(t, primary).URL+"/v1")
	b := gatewayConnection(t, s, "B", newFakeGateway(t, backup).URL+"/v1")
	s.SetGatewaySettings(GatewaySettings{PrimaryID: a.ID, BackupIDs: []string{b.ID}, AutoReroute: true})
	s.SetImageConcurrency(1)
	s.CheckGateways()
	submitN(t, s, a, 3)
	// Never sent (paused before submitting) and long ago: neither is a call this week.
	s.mu.Lock()
	s.store.Put("task", "never-sent", Task{ID: "never-sent", Status: "blocked", Connection: a, CreatedAt: Now(), AssetIDs: []string{}})
	old := Task{ID: "old-call", BatchID: "old", Status: "succeeded", Connection: b, Model: "test-image", CreatedAt: Now(), AssetIDs: []string{}}
	old.Diagnostic = GenerationDiagnostic{StartedAt: time.Now().AddDate(0, 0, -40).UTC().Format(time.RFC3339Nano), ElapsedMS: 1000, HTTPStatus: 200}
	s.store.Put("task", old.ID, old)
	s.mu.Unlock()
	s.Start(func() {})
	waitFor(t, "set finished", func() bool { return countStatus(s, "succeeded") == 3 })

	m, e := s.GatewayMonitor(MonitorQuery{Days: 7})
	if e != nil {
		t.Fatal(e)
	}
	if m.Total != 3 || m.Succeeded != 2 || m.Unknown != 1 || m.Rerouted != 2 {
		t.Fatalf("totals: %+v", m)
	}
	if len(m.Gateways) != 2 || m.Gateways[0].ConnectionID != a.ID || m.Gateways[0].Role != "primary" || m.Gateways[1].Role != "backup" || m.Gateways[1].BackupOrder != 1 {
		t.Fatalf("gateways: %+v", m.Gateways)
	}
	ga, gb := m.Gateways[0], m.Gateways[1]
	if ga.Requests != 1 || ga.Unknown != 1 || ga.RoutedOut != 2 || gb.Requests != 2 || gb.Succeeded != 2 || gb.RoutedIn != 2 || gb.MedianMS <= 0 {
		t.Fatalf("per gateway: A=%+v B=%+v", ga, gb)
	}
	today := m.Days[len(m.Days)-1]
	if len(m.Days) != 7 || today.Counts[a.ID] != 1 || today.Counts[b.ID] != 2 {
		t.Fatalf("daily counts: %+v", today)
	}
	if m.CallTotal != 3 || !strings.HasPrefix(m.Calls[0].SourceLabel, "生图工作台 · 批次 ") {
		t.Fatalf("calls: %+v", m.Calls)
	}
	cut, _ := s.GatewayMonitor(MonitorQuery{Days: 7, Result: "unknown"})
	if cut.CallTotal != 1 || cut.Calls[0].ConnectionID != a.ID || cut.Calls[0].HTTPStatus != 524 {
		t.Fatalf("result filter: %+v", cut.Calls)
	}
	onB, _ := s.GatewayMonitor(MonitorQuery{Days: 7, ConnectionID: b.ID})
	if onB.Total != 2 || len(onB.Gateways) != 1 || onB.Calls[0].RoutedFromName != "A" {
		t.Fatalf("gateway filter: %+v", onB)
	}
	all, _ := s.GatewayMonitor(MonitorQuery{})
	if all.Total != 4 {
		t.Fatal("the 40-day-old call belongs to “全部”", all.Total)
	}

	path := filepath.Join(t.TempDir(), "调用记录.csv")
	n, e := s.ExportGatewayCalls(MonitorQuery{Days: 7}, path)
	if e != nil || n != 3 {
		t.Fatal(n, e)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.HasPrefix(text, "\xef\xbb\xbf时间,中转站") || strings.Count(text, "\n") != 4 || !strings.Contains(text, "结果未知") || !strings.Contains(text, ",A,") {
		t.Fatalf("csv: %s", text)
	}
	if strings.Contains(text, "test-key") {
		t.Fatal("API key in export")
	}
}

func TestCSVCellsCannotRunAsFormulas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.csv")
	if _, e := writeCSV(path, [][]string{{"名称"}, {"=HYPERLINK(\"x\")"}}); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "'=HYPERLINK") {
		t.Fatal(string(data))
	}
}

func TestLLMMonitorRecordsBridgeRequests(t *testing.T) {
	s := testService(t)
	var n atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n.Add(1) {
		case 1: // a normal streamed reply with usage
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("request-id", "req-stream-1")
			w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":100,\"cache_read_input_tokens\":20,\"output_tokens\":1}}}\n\n"))
			w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":30}}\n\n"))
			w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
		case 2: // the connection drops before the first event
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
		case 3: // retried without streaming
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":50,"output_tokens":7}}`))
		default:
			w.WriteHeader(529)
			w.Write([]byte(`{"error":{"message":"overloaded"}}`))
		}
	}))
	defer up.Close()
	llm := saveLLM(t, s, up.URL+"/v1")
	s.store.Get("connection", llm.ID, &llm)
	run := AgentRun{ID: NewID(), Status: "submitted", AuthorizedCount: 12, LLMConnection: llm, Input: AgentInput{Instruction: "圣诞卫衣套图 12 张"}, Events: []AgentEvent{{Now(), "分析参考图"}, {Now(), "等待 LLM 响应"}}}
	s.store.Put("agent_run", run.ID, run)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	base, token, stop, e := startAgentBridge(ctx, s, run, "llm-secret-456")
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	for _, body := range []string{`{"model":"mock-model","stream":true}`, `{"model":"mock-model","stream":true}`, `{"model":"mock-model"}`, `{"model":"mock-model"}`} {
		req, _ := http.NewRequestWithContext(ctx, "POST", base+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if resp, e := http.DefaultClient.Do(req); e == nil {
			resp.Body.Close()
		}
	}
	waitFor(t, "four calls recorded", func() bool {
		calls, _ := List[LLMCall](s.store, "llm_call")
		return len(calls) == 4
	})
	m, e := s.LLMMonitor(MonitorQuery{Days: 7})
	if e != nil {
		t.Fatal(e)
	}
	if m.Requests != 4 || m.Dropped != 1 || m.Errors != 1 || m.ErrorNote != "HTTP 529" || m.InputTokens != 170 || m.OutputTokens != 37 || m.AnalysisRuns != 1 {
		t.Fatalf("totals: %+v", m)
	}
	if len(m.Providers) != 1 || m.Providers[0].Completed != 2 || m.Providers[0].AnalysisRuns != 1 {
		t.Fatalf("providers: %+v", m.Providers)
	}
	if len(m.Runs) != 1 || m.Runs[0].Requests != 4 || m.Runs[0].Dropped != 1 || m.Runs[0].Result != "已提交 12 张" || m.Runs[0].Name != "圣诞卫衣套图 12 张" {
		t.Fatalf("runs: %+v", m.Runs)
	}
	if len(m.Drops) != 1 || !m.Drops[0].Recovered || m.Drops[0].Stage != "分析参考图" {
		t.Fatalf("drops: %+v", m.Drops)
	}
	stream := m.Calls[len(m.Calls)-1] // oldest
	if stream.Status != "completed" || stream.RequestID != "req-stream-1" || stream.InputTokens != 120 || stream.OutputTokens != 30 || stream.FirstByteMS <= 0 || !stream.Stream {
		t.Fatalf("stream call: %+v", stream)
	}
	if m.Calls[0].Status != "error" || m.Calls[0].HTTPStatus != 529 {
		t.Fatalf("error call: %+v", m.Calls[0])
	}
	for _, c := range m.Calls {
		if strings.Contains(c.Error, "llm-secret-456") {
			t.Fatal("key in call record")
		}
	}
	tests, _ := s.LLMMonitor(MonitorQuery{Source: "test"})
	if tests.Requests != 0 {
		t.Fatal("analysis requests counted as tests")
	}
	path := filepath.Join(t.TempDir(), "llm.csv")
	if n, e := s.ExportLLMCalls(MonitorQuery{Result: "dropped"}, path); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}

func TestDropReasonsInPlainWords(t *testing.T) {
	cases := map[string]string{
		"read tcp 127.0.0.1:61108->127.0.0.1:10808: wsarecv: An existing connection was forcibly closed by the remote host.": "本机代理断开连接",
		"read tcp 10.0.0.2:5000->1.2.3.4:443: connection reset by peer":                                                      "连接被重置",
		"unexpected EOF":            "回复途中连接被关闭",
		"context deadline exceeded": "等待超时",
	}
	for in, want := range cases {
		if got := dropReason(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
