package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func saveLLM(t *testing.T, s *Service, base string) Connection {
	t.Helper()
	c, e := s.SaveConnection(ConnectionInput{Kind: "llm", Name: "LLM 测试", BaseURL: base, Model: "mock-model", Key: "llm-secret-456", Mode: "compatible", AuthMode: "api-key"})
	if e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	var saved Connection
	s.store.Get("connection", c.ID, &saved)
	saved.Capabilities = LLMCapabilities{Model: c.Model, Text: true, Vision: true, Tools: true, Streaming: true, CheckedAt: Now()}
	s.store.Put("connection", c.ID, saved)
	s.mu.Unlock()
	return c
}
func agentInput(llm, imageConn Connection) AgentInput {
	return AgentInput{Instruction: "规划至少12张 Etsy 套图，衣服比例和原字体锁定。", LLMConnectionID: llm.ID, ImageConnectionID: imageConn.ID, TargetCount: 12, Size: "1024x1024", Quality: "auto", Ratio: "1:1", References: []string{}, SkillIDs: []string{"etsy-suite"}}
}
func simplePlan(count int, refs []string) SuitePlan {
	cuff := true
	p := SuitePlan{Locked: []string{"比例"}, Editable: []string{"背景"}, Items: []SuiteItem{}, CuffEmbroidery: &cuff}
	for i := 0; i < count; i++ {
		p.Items = append(p.Items, SuiteItem{Purpose: fmt.Sprintf("图%d", i+1), Scene: "客厅", Prompt: fmt.Sprintf("test prompt %d", i+1), References: refs, Size: "1024x1024", Quality: "auto"})
	}
	return p
}
func TestConnectionKindsAndCapabilityInvalidation(t *testing.T) {
	s := testService(t)
	imageConn := saveConnection(t, s, "https://example.com/v1")
	llm := saveLLM(t, s, "https://example.com/anthropic/v1")
	if _, e := s.Submit(request(llm)); e == nil {
		t.Fatal("LLM used for images")
	}
	if _, e := s.SaveConnection(ConnectionInput{ID: imageConn.ID, Name: imageConn.Name, BaseURL: imageConn.BaseURL, Kind: "llm"}); e == nil {
		t.Fatal("connection kind changed")
	}
	llm, e := s.SaveConnection(ConnectionInput{ID: llm.ID, Kind: "llm", Name: llm.Name, BaseURL: llm.BaseURL, Model: "changed", Mode: "compatible", AuthMode: "api-key"})
	if e != nil || llm.Capabilities.Vision {
		t.Fatal("stale capability retained", e)
	}
	legacy := Connection{ID: "legacy", BaseURL: imageConn.BaseURL}
	s.store.Put("connection", legacy.ID, legacy)
	all, _ := s.ListConnections()
	for _, c := range all {
		if c.ID == "legacy" && c.Kind != "image" {
			t.Fatal("legacy not normalized")
		}
	}
}

func TestAgentCancellationContinuationAndBusyDataOperations(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/v1")
	images := saveConnection(t, s, "https://example.com/v1")
	entered := make(chan struct{})
	s.agentRunner = func(ctx context.Context, in agentInvocation) error { close(entered); <-ctx.Done(); return ctx.Err() }
	id, e := s.StartAgentAnalysis(agentInput(llm, images))
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	if _, e = s.StartAgentAnalysis(agentInput(llm, images)); e == nil {
		t.Fatal("concurrent agent allowed")
	}
	if e = s.ChangeDataDirectory(filepath.Join(t.TempDir(), "next"), func(string) error { return nil }); e == nil {
		t.Fatal("migration during analysis")
	}
	if e = s.CreateBackup(filepath.Join(t.TempDir(), "backup.zip")); e == nil {
		t.Fatal("backup during analysis")
	}
	if e = s.DeleteConnection(llm.ID); e == nil {
		t.Fatal("active LLM connection deleted")
	}
	if e = s.CancelAgentAnalysis(id); e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	r, _ := s.GetAgentRun(id)
	if r.Status != "interrupted" {
		t.Fatal(r.Status)
	}
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		if !strings.Contains(in.Prompt, "真实尺寸 50cm") {
			return fmt.Errorf("continuation omitted supplied data")
		}
		if strings.Join(in.Env, "\n") == "" || strings.Contains(strings.Join(in.Env, "\n"), "llm-secret-456") {
			return fmt.Errorf("real supplier key passed to child")
		}
		if e := s.updateAgent(id, "套图方案已保存", func(run *AgentRun) error {
			p := simplePlan(12, []string{})
			if e := s.validatePlan(run, &p); e != nil {
				return e
			}
			run.Plans = append(run.Plans, p)
			return nil
		}); e != nil {
			return e
		}
		in.Event([]byte(`{"type":"result","is_error":false}`))
		return nil
	}
	if e = s.ContinueAgentAnalysis(AgentContinuation{RunID: id, ProductText: "真实尺寸 50cm", Instruction: "使用已补充的尺寸"}); e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	r, _ = s.GetAgentRun(id)
	if r.Status != "review" || len(r.Plans) != 1 || len(r.Instructions) != 1 {
		t.Fatal(r)
	}
}

func TestSuiteCollisionRollbackAndRetryRemainsInSuite(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/v1")
	images := saveConnection(t, s, "https://example.com/v1")
	makeRun := func() AgentRun {
		r := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "review", Input: agentInput(llm, images), Plans: []SuitePlan{}}
		s.store.Put("agent_run", r.ID, r)
		if _, e := s.SaveSuitePlan(r.ID, simplePlan(12, []string{})); e != nil {
			t.Fatal(e)
		}
		return r
	}
	first := makeRun()
	batch, e := s.SubmitSuite(first.ID, 1, "same-request")
	if e != nil {
		t.Fatal(e)
	}
	second := makeRun()
	if _, e = s.SubmitSuite(second.ID, 1, "same-request"); e == nil {
		t.Fatal("reused request id accepted")
	}
	tasks, _ := s.GetAgentTasks(second.ID)
	if len(tasks) != 0 {
		t.Fatal("partial transaction left tasks")
	}
	saved, _ := s.GetAgentRun(second.ID)
	if saved.BatchID != "" {
		t.Fatal("failed transaction linked batch")
	}
	tasks, _ = s.GetAgentTasks(first.ID)
	old := tasks[0]
	old.Status = "unknown"
	s.store.Put("task", old.ID, old)
	retry, e := s.Retry(old.ID)
	if e != nil || retry != batch {
		t.Fatal("retry escaped suite", e)
	}
	tasks, _ = s.GetAgentTasks(first.ID)
	if len(tasks) != 12 || tasks[0].ID == old.ID || tasks[0].PlanItemID != old.PlanItemID {
		t.Fatal("suite did not select latest attempt")
	}
}
func TestSuiteConfirmationAtomicPerImageAndDedup(t *testing.T) {
	s := testService(t)
	imageConn := saveConnection(t, s, "https://example.com/v1")
	llm := saveLLM(t, s, "https://example.com/v1")
	a, _ := s.ImportImage("one.png", base64.StdEncoding.EncodeToString(tinyPNG()))
	b, _ := s.ImportImage("two.png", base64.StdEncoding.EncodeToString(tinyPNG()))
	in := agentInput(llm, imageConn)
	in.References = []string{a.ID, b.ID}
	r := AgentRun{ID: NewID(), Status: "review", Input: in, CreatedAt: Now(), LLMConnection: llm, Plans: []SuitePlan{}, Questions: []AgentQuestion{{ID: "p004", Required: true, Label: "P004 工艺"}}}
	s.store.Put("agent_run", r.ID, r)
	p := simplePlan(12, []string{a.ID})
	p.Items[1].References = []string{b.ID}
	saved, e := s.SaveSuitePlan(r.ID, p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SubmitSuite(r.ID, saved.Revision, "request"); e == nil {
		t.Fatal("missing information allowed")
	}
	page, _ := s.ListTasks("", 0, 100)
	if len(page.Items) != 0 {
		t.Fatal("generation before confirmation")
	}
	s.store.Get("agent_run", r.ID, &r)
	r.Questions[0].Answer = "真实客户机绣"
	s.store.Put("agent_run", r.ID, r)
	batch, e := s.SubmitSuite(r.ID, saved.Revision, "request")
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.SubmitSuite(r.ID, saved.Revision, "request")
	if e != nil || again != batch {
		t.Fatal("not deduplicated", e)
	}
	tasks, _ := s.GetAgentTasks(r.ID)
	if len(tasks) != 12 {
		t.Fatalf("%d tasks", len(tasks))
	}
	for _, task := range tasks {
		if task.BatchID != batch || len(task.References) != 1 {
			t.Fatal(task)
		}
		if strings.HasPrefix(task.Prompt, "test prompt 2\n") && task.References[0] != b.ID {
			t.Fatal("per-item references lost")
		}
	}
	if _, e = s.SaveSuitePlan(r.ID, p); e == nil {
		t.Fatal("edited already submitted plan")
	}
}
func TestSkillImportFreezingAndBackup(t *testing.T) {
	s := testService(t)
	source := t.TempDir()
	content := "---\nname: color-rules\ndescription: 真实线色规则\n---\n使用附件色卡，不能编造。"
	os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(content), 0600)
	os.WriteFile(filepath.Join(source, "colors.json"), []byte(`{"black":"#000000"}`), 0600)
	v, e := s.ImportSkill(source, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ImportSkill(source, false); e == nil {
		t.Fatal("silent overwrite")
	}
	r := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "review", Skills: []SkillRecord{v}, LLMConnection: Connection{Credential: "encrypted-ref", HasKey: true}}
	s.store.Put("agent_run", r.ID, r)
	v.Content = strings.ReplaceAll(v.Content, "不能编造", "只使用黑色")
	v, e = s.SaveSkill(v)
	if e != nil || v.Version != 2 {
		t.Fatal(e)
	}
	old, _ := s.GetAgentRun(r.ID)
	if old.Skills[0].Version != 1 || !strings.Contains(old.Skills[0].Content, "不能编造") {
		t.Fatal("skill snapshot mutated")
	}
	backup := filepath.Join(t.TempDir(), "backup.zip")
	if e = s.CreateBackup(backup); e != nil {
		t.Fatal(e)
	}
	z, _ := zip.OpenReader(backup)
	for _, f := range z.File {
		if strings.Contains(f.Name, "agent-cache") || strings.Contains(f.Name, "secrets") {
			t.Fatal("secrets/cache backed up")
		}
	}
	z.Close()
	if e = s.RestoreBackup(backup); e != nil {
		t.Fatal(e)
	}
	restored, _ := s.GetAgentRun(r.ID)
	if restored.LLMConnection.HasKey {
		t.Fatal("restored credentials")
	}
	data, e := s.readSkillAttachment(restored.Skills[0].Attachments[0].Path)
	if e != nil || !bytes.Contains(data, []byte("black")) {
		t.Fatal("skill attachment missing", e)
	}
	for _, bad := range []string{strings.Replace(content, "使用附件色卡", "!`whoami`", 1), strings.Replace(content, "description:", "hooks: bad\ndescription:", 1)} {
		if _, _, e := validateSkill(bad); e == nil {
			t.Fatal("unsafe skill accepted")
		}
	}
	os.WriteFile(filepath.Join(source, "evil.ps1"), []byte("echo bad"), 0600)
	if _, e = s.ImportSkill(source, true); e == nil {
		t.Fatal("script imported")
	}
}
func TestProductExtractionAndPartialFailure(t *testing.T) {
	s := testService(t)
	s.allowLocalProduct = true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good":
			io.WriteString(w, `<script type="application/ld+json">{"@type":"Product","name":"定制卫衣","description":"真实商品描述，使用客户提供的工艺和颜色。","image":["/image.png","/missing.png"],"color":"black"}</script>`)
		case "/image.png":
			w.Write(tinyPNG())
		default:
			w.WriteHeader(403)
		}
	}))
	defer server.Close()
	r := AgentRun{ID: NewID(), Input: AgentInput{ProductURL: server.URL + "/good"}, CreatedAt: Now()}
	s.store.Put("agent_run", r.ID, r)
	info, e := s.fetchProduct(context.Background(), r.ID)
	// The page was read; only one image is missing, which is a note, not a page error.
	if e != nil || info.Title != "定制卫衣" || len(info.ReferenceIDs) != 1 || info.Error != "" || info.ImageNote == "" {
		t.Fatal(info, e)
	}
	firstReference := info.ReferenceIDs[0]
	again, e := s.fetchProduct(context.Background(), r.ID)
	if e != nil || len(again.ReferenceIDs) != 1 || again.ReferenceIDs[0] != firstReference {
		t.Fatal("partial fetch duplicated or replaced saved references", e)
	}
	r.Input.ProductURL = server.URL + "/blocked"
	r.Product = ProductInfo{}
	s.store.Put("agent_run", r.ID, r)
	info, e = s.fetchProduct(context.Background(), r.ID)
	if e != nil || !strings.Contains(info.Error, "403") {
		t.Fatal(info, e)
	}
	if publicProductURL(context.Background(), "http://127.0.0.1/test") == nil {
		t.Fatal("private product target allowed")
	}
}

func TestVisionCopyIsBoundedWithoutChangingReference(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2400, 1600))
	var data bytes.Buffer
	png.Encode(&data, img)
	original := append([]byte{}, data.Bytes()...)
	block, e := visionImageBlock(data.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	raw, e := base64.StdEncoding.DecodeString(block["data"].(string))
	if e != nil {
		t.Fatal(e)
	}
	config, _, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || config.Width > 1568 || config.Height > 1568 || !bytes.Equal(original, data.Bytes()) {
		t.Fatal("vision copy mutated original or exceeded limit", e)
	}
}
func TestBridgeAuthenticationCompatibilityAndSSEBoundary(t *testing.T) {
	s := testService(t)
	var upstreamCalls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if r.URL.Path != "/custom/v1/messages" || r.Header.Get("x-api-key") != "llm-secret-456" {
			t.Error("route/auth lost")
		}
		var v map[string]any
		json.NewDecoder(r.Body).Decode(&v)
		if _, ok := v["thinking"]; ok {
			t.Error("compatibility thinking retained")
		}
		if r.Header.Get("anthropic-beta") != "" {
			t.Error("beta forwarded")
		}
		messages := v["messages"].([]any)
		thinking := messages[0].(map[string]any)["content"].([]any)[0].(map[string]any)
		if thinking["thinking"] != "Preserve this reasoning." || thinking["signature"] != "mock-signature" {
			t.Error("thinking history or signature lost during tool round trip")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer up.Close()
	llm := saveLLM(t, s, up.URL+"/custom/v1")
	s.store.Get("connection", llm.ID, &llm)
	r := AgentRun{ID: NewID(), LLMConnection: llm}
	s.store.Put("agent_run", r.ID, r)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	base, token, stop, e := startAgentBridge(ctx, s, r, "llm-secret-456")
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	unauth, _ := http.Post(base+"/v1/messages", "application/json", strings.NewReader(`{}`))
	unauth.Body.Close()
	if unauth.StatusCode != 401 || upstreamCalls.Load() != 0 {
		t.Fatal("unauthenticated bridge forwarded")
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", base+"/v1/messages", strings.NewReader(`{"thinking":{"type":"adaptive"},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"Preserve this reasoning.","signature":"mock-signature"},{"type":"tool_use","id":"tool_1","name":"get_context","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"context"}]}]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "test")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	data, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || !bytes.Contains(data, []byte("message_stop")) {
		t.Fatal("bridge waited for EOF", e)
	}
}
func TestCapabilityProbeUsesActualImageAndToolArguments(t *testing.T) {
	s := testService(t)
	var requests atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var v map[string]any
		json.NewDecoder(r.Body).Decode(&v)
		if choice, ok := v["tool_choice"].(map[string]any); ok && choice["type"] != "auto" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"Thinking mode does not support this tool_choice"}}`)
			return
		}
		if v["stream"] == true {
			writeFakeMessage(w, []messageBlock{{Type: "text", Text: "OK"}}, "")
			return
		}
		content := v["messages"].([]any)[0].(map[string]any)["content"]
		block := messageBlock{Type: "text", Text: "OK"}
		if parts, ok := content.([]any); ok {
			source := parts[0].(map[string]any)["source"].(map[string]any)
			raw, _ := base64.StdEncoding.DecodeString(source["data"].(string))
			img, _, _ := image.Decode(bytes.NewReader(raw))
			red, _, blue, _ := img.At(0, 0).RGBA()
			block.Text = "BLUE"
			if red > blue {
				block.Text = "RED"
			}
		}
		if v["tools"] != nil {
			if v["max_tokens"].(float64) != 2048 {
				t.Error("tool probe has insufficient reasoning budget")
			}
			nonce := strings.TrimPrefix(content.(string), "Call capability_echo with nonce ")
			block = messageBlock{Type: "tool_use", Name: "capability_echo", Input: map[string]any{"nonce": nonce}}
		}
		json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "thinking", "thinking": "Checking the requested capability.", "signature": "mock-signature"}, block}})
	}))
	defer up.Close()
	c := saveLLM(t, s, up.URL)
	cap, e := s.ValidateLLMCapabilities(c.ID, "probe")
	if e != nil || !cap.Text || !cap.Vision || !cap.Tools || !cap.Streaming {
		t.Fatal(cap, e)
	}
	if requests.Load() != 4 {
		t.Fatal("validation retried or exceeded four requests", requests.Load())
	}
}

func writeFakeMessage(w http.ResponseWriter, blocks []messageBlock, toolID string) {
	writeFakeMessageWithThinking(w, blocks, toolID, false)
}
func writeFakeMessageWithThinking(w http.ResponseWriter, blocks []messageBlock, toolID string, thinking bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	emit := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", v.(map[string]any)["type"], b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	emit(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_mock", "type": "message", "role": "assistant", "model": "mock-model", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 100, "output_tokens": 0}}})
	reason := "end_turn"
	offset := 0
	if thinking {
		emit(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
		emit(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": "Planning the next step."}})
		emit(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "signature_delta", "signature": "mock-signature"}})
		emit(map[string]any{"type": "content_block_stop", "index": 0})
		offset = 1
	}
	for i, b := range blocks {
		i += offset
		start := map[string]any{"type": b.Type}
		var delta map[string]any
		if b.Type == "tool_use" {
			reason = "tool_use"
			start["id"] = toolID
			start["name"] = b.Name
			start["input"] = map[string]any{}
			raw, _ := json.Marshal(b.Input)
			delta = map[string]any{"type": "input_json_delta", "partial_json": string(raw)}
		} else {
			start["text"] = ""
			delta = map[string]any{"type": "text_delta", "text": b.Text}
		}
		emit(map[string]any{"type": "content_block_start", "index": i, "content_block": start})
		emit(map[string]any{"type": "content_block_delta", "index": i, "delta": delta})
		emit(map[string]any{"type": "content_block_stop", "index": i})
	}
	emit(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 100}})
	emit(map[string]any{"type": "message_stop"})
}
func TestNativeClaudeSkillsMCPAndImagePlanning(t *testing.T) {
	exe := os.Getenv("IMAGEWORKBENCH_NATIVE_AGENT")
	if exe == "" {
		exe, _ = filepath.Abs("../../.tools/claude-code/claude.exe")
	}
	if _, e := os.Stat(exe); e != nil {
		t.Skip("native Claude runtime not prepared")
	}
	s := testService(t)
	s.SetAgentExecutable(exe)
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		isolated := []string{}
		for _, env := range in.Env {
			name, _, _ := strings.Cut(env, "=")
			if !strings.EqualFold(name, "PATH") {
				isolated = append(isolated, env)
			}
		}
		in.Env = append(isolated, "PATH="+filepath.Join(os.Getenv("SystemRoot"), "System32"))
		return runNativeAgent(ctx, in)
	}

	var calls atomic.Int32
	var sawImage atomic.Bool
	var sawSkill atomic.Bool
	ref, _ := s.ImportImage("reference.png", base64.StdEncoding.EncodeToString(tinyPNG()))
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") != "llm-secret-456" {
			t.Error("upstream key missing")
		}
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("Launching skill: listing-suite")) {
			sawSkill.Store(true)
		}
		if bytes.Contains(body, []byte(`"type":"image"`)) {
			sawImage.Store(true)
		}
		index := calls.Add(1) - 1
		if index > 0 && (!bytes.Contains(body, []byte(`"thinking":"Planning the next step."`)) || !bytes.Contains(body, []byte(`"signature":"mock-signature"`))) {
			t.Error("native thinking history was not forwarded with tool results")
		}
		var block messageBlock
		switch index {
		case 0:
			block = messageBlock{Type: "tool_use", Name: "mcp__workbench__get_run_context", Input: map[string]any{}}
		case 1:
			block = messageBlock{Type: "tool_use", Name: "Skill", Input: map[string]any{"skill": "listing-suite"}}
		case 2:
			block = messageBlock{Type: "tool_use", Name: "mcp__workbench__read_reference_image", Input: map[string]any{"assetID": ref.ID}}
		case 3:
			raw, _ := json.Marshal(simplePlan(12, []string{ref.ID}))
			var plan map[string]any
			json.Unmarshal(raw, &plan)
			block = messageBlock{Type: "tool_use", Name: "mcp__workbench__save_suite_plan", Input: plan}
		default:
			block = messageBlock{Type: "text", Text: "已保存12张套图方案，由后台提交生图。"}
		}
		writeFakeMessageWithThinking(w, []messageBlock{block}, fmt.Sprintf("tool_%d", index), true)
	}))
	defer up.Close()
	llm := saveLLM(t, s, up.URL)
	imageConn := saveConnection(t, s, up.URL)
	in := agentInput(llm, imageConn)
	in.References = []string{ref.ID}
	id, e := s.StartSmartGeneration(in, "native-smart")
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(70 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := s.GetAgentRun(id)
		if r.Status != "analyzing" {
			if r.Status != "submitted" || len(r.Plans) != 1 || len(r.Plans[0].Items) != 12 {
				t.Fatalf("native flow failed: %+v, calls=%d", r, calls.Load())
			}
			if !sawSkill.Load() {
				t.Fatal("selected Skill did not run")
			}
			if !sawImage.Load() {
				t.Fatal("image did not reach LLM")
			}
			page, _ := s.ListTasks("", 0, 100)
			if len(page.Items) != 12 {
				t.Fatal("native orchestration did not enqueue twelve images")
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.CancelAgentAnalysis(id)
	t.Fatal("native agent did not finish")
}
