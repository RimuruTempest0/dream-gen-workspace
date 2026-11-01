package core

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGateway is a relay whose model list and generation result are set per test.
type fakeGateway struct {
	models    []string
	modelsErr int  // HTTP status for /models, 0 = list models
	timeout   bool // generation answers HTTP 524 like Cloudflare
	// refuse answers generation with this status and message, before generating.
	refuse        int
	refuseMessage string
	modelHits     atomic.Int32
	imageHits     atomic.Int32
}

func newFakeGateway(t *testing.T, g *fakeGateway) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if strings.HasSuffix(r.URL.Path, "/models") {
			g.modelHits.Add(1)
			if g.modelsErr != 0 {
				w.WriteHeader(g.modelsErr)
				w.Write([]byte(`{"error":{"message":"denied"}}`))
				return
			}
			data := []map[string]string{}
			for _, m := range g.models {
				data = append(data, map[string]string{"id": m})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/usage") {
			w.WriteHeader(404) // the free quota read is not an image request
			return
		}
		g.imageHits.Add(1)
		if g.refuse != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(g.refuse)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": g.refuseMessage}})
			return
		}
		time.Sleep(20 * time.Millisecond) // a measurable generation time
		if g.timeout {
			w.WriteHeader(524)
			w.Write([]byte("error code: 524"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG())}}})
	}))
	t.Cleanup(server.Close)
	return server
}

func gatewayConnection(t *testing.T, s *Service, name, base string) Connection {
	t.Helper()
	c, e := s.SaveConnection(ConnectionInput{Name: name, BaseURL: base, Model: "test-image", Key: "test-key"})
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func shortHealthWindows(t *testing.T, window, quiet time.Duration) {
	oldWindow, oldQuiet := healthWindow, recoverQuiet
	healthWindow, recoverQuiet = window, quiet
	t.Cleanup(func() { healthWindow, recoverQuiet = oldWindow, oldQuiet })
}

func tasksWhere(s *Service, ok func(Task) bool) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, _ := List[Task](s.store, "task")
	out := []Task{}
	for _, t := range all {
		if ok(t) {
			out = append(out, t)
		}
	}
	return out
}

func submitN(t *testing.T, s *Service, c Connection, n int) {
	t.Helper()
	in := request(c)
	in.Count = n
	if _, e := s.Submit(in); e != nil {
		t.Fatal(e)
	}
}

func TestGatewaySettingsDefaultsValidationAndDelete(t *testing.T) {
	s := testService(t)
	g, _ := s.GetGatewaySettings()
	if !g.AutoReroute || g.AutoRegenerate524 || g.PrimaryID != "" || len(g.BackupIDs) != 0 {
		t.Fatalf("unexpected defaults: %+v", g)
	}
	a := gatewayConnection(t, s, "A", "http://127.0.0.1:1/v1")
	b := gatewayConnection(t, s, "B", "http://127.0.0.1:2/v1")
	llm, e := s.SaveConnection(ConnectionInput{Kind: "llm", Name: "LLM", BaseURL: "http://127.0.0.1:3/v1", Model: "m", Key: "k"})
	if e != nil {
		t.Fatal(e)
	}
	if s.SetGatewaySettings(GatewaySettings{PrimaryID: llm.ID}) == nil {
		t.Fatal("LLM connection accepted as image gateway")
	}
	if s.SetGatewaySettings(GatewaySettings{PrimaryID: a.ID, BackupIDs: []string{"missing"}}) == nil {
		t.Fatal("unknown backup accepted")
	}
	if e = s.SetGatewaySettings(GatewaySettings{PrimaryID: a.ID, BackupIDs: []string{a.ID, b.ID, b.ID}, AutoReroute: true}); e != nil {
		t.Fatal(e)
	}
	g, _ = s.GetGatewaySettings()
	if g.PrimaryID != a.ID || len(g.BackupIDs) != 1 || g.BackupIDs[0] != b.ID {
		t.Fatalf("primary must not be a backup and backups are unique: %+v", g)
	}
	if e = s.DeleteConnection(b.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteConnection(a.ID); e != nil {
		t.Fatal(e)
	}
	g, _ = s.GetGatewaySettings()
	if g.PrimaryID != "" || len(g.BackupIDs) != 0 {
		t.Fatalf("deleted connections left in settings: %+v", g)
	}
}

func TestFreeCheckOnlyReadsModelList(t *testing.T) {
	s := testService(t)
	ok := &fakeGateway{models: []string{"test-image"}}
	denied := &fakeGateway{modelsErr: 401}
	unlisted := &fakeGateway{modelsErr: 404}
	a := gatewayConnection(t, s, "A", newFakeGateway(t, ok).URL+"/v1")
	b := gatewayConnection(t, s, "B", newFakeGateway(t, denied).URL+"/v1")
	c := gatewayConnection(t, s, "C", newFakeGateway(t, unlisted).URL+"/v1")
	health, e := s.CheckGateways()
	if e != nil {
		t.Fatal(e)
	}
	states := map[string]GatewayHealth{}
	for _, h := range health {
		states[h.ConnectionID] = h
	}
	if states[a.ID].State != "healthy" || !states[a.ID].ModelsOK || states[a.ID].ModelCount != 1 {
		t.Fatalf("A: %+v", states[a.ID])
	}
	if states[b.ID].State != "down" || !strings.Contains(states[b.ID].Reason, "401") {
		t.Fatalf("B: %+v", states[b.ID])
	}
	// A relay without a model list is not "down", but cannot serve as a backup.
	if states[c.ID].State == "down" || states[c.ID].ModelsOK {
		t.Fatalf("C: %+v", states[c.ID])
	}
	for _, g := range []*fakeGateway{ok, denied, unlisted} {
		if g.imageHits.Load() != 0 || g.modelHits.Load() != 1 {
			t.Fatal("the free check must only read the model list", g.imageHits.Load(), g.modelHits.Load())
		}
	}
}

func TestUnsentImagesMoveToHealthyBackupWithoutResending(t *testing.T) {
	s := testService(t)
	primary := &fakeGateway{models: []string{"test-image"}, timeout: true}
	backup := &fakeGateway{models: []string{"test-image"}}
	a := gatewayConnection(t, s, "A", newFakeGateway(t, primary).URL+"/v1")
	b := gatewayConnection(t, s, "B", newFakeGateway(t, backup).URL+"/v1")
	if e := s.SetGatewaySettings(GatewaySettings{PrimaryID: a.ID, BackupIDs: []string{b.ID}, AutoReroute: true}); e != nil {
		t.Fatal(e)
	}
	s.SetImageConcurrency(1)
	if _, e := s.CheckGateways(); e != nil {
		t.Fatal(e)
	}
	submitN(t, s, a, 3)
	s.Start(func() {})
	waitFor(t, "two images saved on the backup", func() bool { return countStatus(s, "succeeded") == 2 })
	time.Sleep(200 * time.Millisecond)
	if primary.imageHits.Load() != 1 || backup.imageHits.Load() != 2 {
		t.Fatal("only unsent images may move; the cut image is never resent", primary.imageHits.Load(), backup.imageHits.Load())
	}
	if countStatus(s, "unknown") != 1 {
		t.Fatal("the timed-out image must stay unknown")
	}
	moved := tasksWhere(s, func(t Task) bool { return t.Status == "succeeded" })
	for _, m := range moved {
		if m.Connection.ID != b.ID || m.RoutedFrom != a.ID || m.RoutedFromName != "A" || !strings.Contains(m.RouteReason, "变慢") {
			t.Fatalf("moved image not recorded: %+v", m)
		}
	}
	cut := tasksWhere(s, func(t Task) bool { return t.Status == "unknown" })[0]
	if cut.AutoRetriedAs != "" || !strings.Contains(cut.Error, "不会自动重发") {
		t.Fatal("auto re-generation must be off by default", cut.Error)
	}
}

func TestBackupNeedsTheExactModel(t *testing.T) {
	s := testService(t)
	primary := &fakeGateway{models: []string{"test-image"}, timeout: true}
	other := &fakeGateway{models: []string{"test-image-v2", "Test-Image"}}
	a := gatewayConnection(t, s, "A", newFakeGateway(t, primary).URL+"/v1")
	b := gatewayConnection(t, s, "B", newFakeGateway(t, other).URL+"/v1")
	s.SetGatewaySettings(GatewaySettings{PrimaryID: a.ID, BackupIDs: []string{b.ID}, AutoReroute: true})
	s.SetImageConcurrency(1)
	s.CheckGateways()
	submitN(t, s, a, 2)
	s.Start(func() {})
	// No backup: the slow gateway keeps one image at a time as its trial.
	waitFor(t, "both tried on the primary", func() bool { return countStatus(s, "unknown") == 2 })
	if other.imageHits.Load() != 0 || primary.imageHits.Load() != 2 {
		t.Fatal("a gateway without the exact model was used", other.imageHits.Load(), primary.imageHits.Load())
	}
}

func TestDownGatewayPausesUnsentImages(t *testing.T) {
	s := testService(t)
	dead := &fakeGateway{modelsErr: 401}
	a := gatewayConnection(t, s, "A", newFakeGateway(t, dead).URL+"/v1")
	s.SetGatewaySettings(GatewaySettings{PrimaryID: a.ID, AutoReroute: true})
	s.CheckGateways()
	submitN(t, s, a, 2)
	s.Start(func() {})
	waitFor(t, "unsent images paused", func() bool { return countStatus(s, "blocked") == 2 })
	if dead.imageHits.Load() != 0 {
		t.Fatal("an image was sent to a gateway that is down")
	}
	paused := tasksWhere(s, func(t Task) bool { return t.Status == "blocked" })[0]
	if !strings.Contains(paused.Error, "未提交、未计费") {
		t.Fatal(paused.Error)
	}
}

func TestAutoRegenerateUsesBackupOnce(t *testing.T) {
	s := testService(t)
	primary := &fakeGateway{models: []string{"test-image"}, timeout: true}
	backup := &fakeGateway{models: []string{"test-image"}}
	a := gatewayConnection(t, s, "A", newFakeGateway(t, primary).URL+"/v1")
	b := gatewayConnection(t, s, "B", newFakeGateway(t, backup).URL+"/v1")
	s.SetGatewaySettings(GatewaySettings{PrimaryID: a.ID, BackupIDs: []string{b.ID}, AutoReroute: true, AutoRegenerate524: true})
	s.CheckGateways()
	submitN(t, s, a, 1)
	s.Start(func() {})
	waitFor(t, "re-generated on the backup", func() bool { return countStatus(s, "succeeded") == 1 })
	time.Sleep(200 * time.Millisecond)
	if primary.imageHits.Load() != 1 || backup.imageHits.Load() != 1 {
		t.Fatal("exactly one extra request on the backup", primary.imageHits.Load(), backup.imageHits.Load())
	}
	cut := tasksWhere(s, func(t Task) bool { return t.Status == "unknown" })[0]
	again := tasksWhere(s, func(t Task) bool { return t.Status == "succeeded" })[0]
	if cut.AutoRetriedAs != again.ID || again.AutoRetryOf != cut.ID || again.Connection.ID != b.ID || again.BatchID != cut.BatchID {
		t.Fatalf("attempts not linked: %+v / %+v", cut, again)
	}
	if !strings.Contains(cut.Error, "两次费用") {
		t.Fatal("cost notice missing", cut.Error)
	}
}

func TestAutoRegenerateCapsAndWaitsForRecovery(t *testing.T) {
	shortHealthWindows(t, 600*time.Millisecond, 400*time.Millisecond)
	s := testService(t)
	only := &fakeGateway{models: []string{"test-image"}, timeout: true}
	a := gatewayConnection(t, s, "A", newFakeGateway(t, only).URL+"/v1")
	s.SetGatewaySettings(GatewaySettings{PrimaryID: a.ID, AutoReroute: true, AutoRegenerate524: true})
	s.SetImageConcurrency(4)
	s.CheckGateways()
	submitN(t, s, a, 5)
	s.Start(func() {})
	waitFor(t, "all five tried", func() bool { return countStatus(s, "unknown") >= 5 })
	// Re-generations never go back into the slow period on the same gateway...
	if hits := only.imageHits.Load(); hits != 5 {
		t.Fatal("re-generation sent while the gateway was slow", hits)
	}
	if n := len(tasksWhere(s, func(t Task) bool { return t.AutoRetryOf != "" })); n != maxAutoRegenPerBatch {
		t.Fatal("per-batch cap", n)
	}
	// ...but are sent once it has recovered, and are never re-generated again.
	waitFor(t, "re-generations sent after recovery", func() bool { return countStatus(s, "unknown") == 8 })
	time.Sleep(time.Second)
	if hits := only.imageHits.Load(); hits != 8 {
		t.Fatal("an automatic attempt was re-generated again", hits)
	}
	if n := len(tasksWhere(s, func(t Task) bool { return t.AutoRetryOf != "" })); n != maxAutoRegenPerBatch {
		t.Fatal("cap exceeded", n)
	}
}

func TestNoGatewaySetupKeepsOldBehaviour(t *testing.T) {
	s := testService(t)
	g := &fakeGateway{models: []string{"test-image"}, timeout: true}
	a := gatewayConnection(t, s, "A", newFakeGateway(t, g).URL+"/v1")
	s.SetImageConcurrency(1)
	submitN(t, s, a, 2)
	s.Start(func() {})
	waitFor(t, "both sent", func() bool { return countStatus(s, "unknown") == 2 })
	if g.modelHits.Load() != 0 || g.imageHits.Load() != 2 {
		t.Fatal("health checks or routing ran without a gateway setup", g.modelHits.Load(), g.imageHits.Load())
	}
}
