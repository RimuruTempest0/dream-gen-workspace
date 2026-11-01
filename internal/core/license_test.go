package core

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"imageworkbench/internal/license"
)

// keyRelay is an image relay that records which key each request used, can
// refuse a key as "insufficient balance" (quota spent), and answers /v1/usage.
type keyRelay struct {
	mu        sync.Mutex
	keys      []string
	empty     map[string]bool
	remaining float64
}

func (k *keyRelay) used(key string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	n := 0
	for _, v := range k.keys {
		if v == key {
			n++
		}
	}
	return n
}

func newKeyRelay(t *testing.T) (*keyRelay, *httptest.Server) {
	k := &keyRelay{empty: map[string]bool{}, remaining: 0.33}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "test-image"}}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/usage") {
			k.mu.Lock()
			rem := k.remaining
			k.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"mode": "quota_limited", "quota": map[string]any{"limit": 0.45, "used": 0.45 - rem, "remaining": rem, "unit": "USD"}})
			return
		}
		k.mu.Lock()
		k.keys = append(k.keys, key)
		empty := k.empty[key]
		k.mu.Unlock()
		if empty {
			w.WriteHeader(403)
			w.Write([]byte(`{"error":{"message":"insufficient balance"}}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG())}}})
	}))
	t.Cleanup(server.Close)
	return k, server
}

type testCustomer struct {
	dir, id, code string
	priv          ed25519.PrivateKey
}

// isolateLicenseState gives the test its own trial-record folder and registry key.
func isolateLicenseState(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	oldDir, oldKey := licenseStateDir, licenseRegistryKey
	licenseStateDir = func() string { return dir }
	licenseRegistryKey = `Software\ImageWorkbench\Test\` + NewID()
	key := licenseRegistryKey
	t.Cleanup(func() {
		deleteTestRegistryKey(key)
		licenseStateDir, licenseRegistryKey = oldDir, oldKey
	})
}

// writeCustomer signs a customer.dat with a fresh admin key the app trusts.
// image-1 uses imageKey; reuse keeps the ID, code and signing key (an upgrade).
func writeCustomer(t *testing.T, base string, trial, revision int, imageKey string, reuse *testCustomer) testCustomer {
	t.Helper()
	c := testCustomer{dir: t.TempDir()}
	if reuse != nil {
		c.id, c.code, c.priv = reuse.id, reuse.code, reuse.priv
	} else {
		pub, priv, _ := ed25519.GenerateKey(nil)
		c.priv = priv
		c.id, _ = license.NewSoftwareID()
		c.code, _ = license.NewCode()
		old := licensePublicKey
		licensePublicKey = base64.StdEncoding.EncodeToString(pub)
		t.Cleanup(func() { licensePublicKey = old })
	}
	check, _ := license.NewCodeCheck(c.code, c.id)
	conns := []license.Connection{
		{Slot: "image-1", Kind: "image", Name: "图片线路 1", BaseURL: base, Model: "test-image", Key: imageKey, UsageURL: base + "/usage"},
		{Slot: "image-2", Kind: "image", Name: "图片线路 2", BaseURL: base, Model: "test-image", Key: "line-image-2"},
		{Slot: "llm-1", Kind: "llm", Name: "LLM 线路 1", BaseURL: base, Model: "m", Key: "line-llm-1", Verified: true},
		{Slot: "llm-2", Kind: "llm", Name: "LLM 线路 2", BaseURL: base, Model: "m", Key: "line-llm-2", Verified: true},
	}
	for i := range conns {
		conns[i].KeyObf, _ = license.ObfuscateKey(conns[i].Key, c.id)
		conns[i].Key = ""
	}
	data, e := license.Sign(license.Profile{SoftwareID: c.id, Customer: "客户 A", TrialImages: trial, Connections: conns, ImageBackups: []string{"image-2"}, LinesRevision: revision, CodeCheck: check}, c.priv)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(c.dir, license.FileName), data, 0o600); e != nil {
		t.Fatal(e)
	}
	return c
}

func imageLine(t *testing.T, s *Service, slot string) Connection {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	cs, _ := List[Connection](s.store, "connection")
	for _, c := range cs {
		if c.Managed == slot {
			return c
		}
	}
	t.Fatal("no line", slot)
	return Connection{}
}

func TestDeveloperModeWithoutCustomerFile(t *testing.T) {
	s := testService(t)
	if e := s.LoadLicense(t.TempDir()); e != nil {
		t.Fatal(e)
	}
	if st := s.LicenseStatus(); st.Mode != "developer" {
		t.Fatal(st)
	}
	if _, e := s.Activate("ABCDE-FGHJK-MNPQR-STVWX-YZ012"); e == nil {
		t.Fatal("developer build accepted a code")
	}
}

func TestTrialUsesTheMainLinesAndStopsAtTheLimit(t *testing.T) {
	isolateLicenseState(t)
	relay, server := newKeyRelay(t)
	s := testService(t)
	c := writeCustomer(t, server.URL+"/v1", 3, 1, "line-image-1", nil)
	if e := s.LoadLicense(c.dir); e != nil {
		t.Fatal(e)
	}
	st := s.LicenseStatus()
	if st.Mode != "trial" || st.SoftwareID != c.id || st.TrialRemaining != 3 {
		t.Fatalf("status: %+v", st)
	}
	if llm := imageLine(t, s, "llm-1"); !llm.Capabilities.Tools || !llm.Capabilities.Vision {
		t.Fatal("verified LLM line needs no capability check")
	}
	// Trial: the main line only, no backups.
	if g, _ := s.GetGatewaySettings(); g.PrimaryID != imageLine(t, s, "image-1").ID || len(g.BackupIDs) != 0 {
		t.Fatalf("trial gateways: %+v", g)
	}
	// Only the main lines are listed or usable, even if the customer picks another.
	if listed, _ := s.ListConnections(); len(listed) != 2 || !mainLine(listed[0]) || !mainLine(listed[1]) {
		t.Fatalf("trial lines: %+v", listed)
	}
	if _, e := s.Submit(request(imageLine(t, s, "image-2"))); e == nil || !strings.Contains(e.Error(), "试用期只能使用") {
		t.Fatal("backup image line used in trial", e)
	}
	smart := AgentInput{Instruction: "x", TargetCount: 1, LLMConnectionID: imageLine(t, s, "llm-2").ID, ImageConnectionID: imageLine(t, s, "image-1").ID}
	if _, e := s.StartSmartGeneration(smart, "trial-llm-2"); e == nil || !strings.Contains(e.Error(), "试用期只能使用") {
		t.Fatal("backup LLM line used in trial", e)
	}
	line := imageLine(t, s, "image-1")
	in := request(line)
	in.Count = 4
	if _, e := s.Submit(in); e == nil || !strings.Contains(e.Error(), "试用剩余 3 张") {
		t.Fatal("over-limit batch accepted", e)
	}
	in.Count = 3
	if _, e := s.Submit(in); e != nil {
		t.Fatal(e)
	}
	s.Start(func() {})
	waitFor(t, "three trial images", func() bool { return countStatus(s, "succeeded") == 3 })
	if _, e := s.Submit(request(line)); e == nil || !strings.Contains(e.Error(), "试用额度已用完") {
		t.Fatal("submitted after the trial", e)
	}
	// Even a task queued directly is stopped before sending.
	s.mu.Lock()
	sneak := Task{ID: "sneak", BatchID: "sneak", Status: "queued", Trial: true, Connection: line, Model: "test-image", Prompt: "p", CreatedAt: Now(), AssetIDs: []string{}}
	s.store.Put("task", sneak.ID, sneak)
	s.mu.Unlock()
	s.signal()
	waitFor(t, "stopped", func() bool { return countStatus(s, "blocked") == 1 })
	if relay.used("line-image-1") != 3 || relay.used("line-image-2") != 0 {
		t.Fatal("trial requests", relay.used("line-image-1"), relay.used("line-image-2"))
	}
}

func TestTrialCountSurvivesANewDataFolderAndTampering(t *testing.T) {
	isolateLicenseState(t)
	_, server := newKeyRelay(t)
	c := writeCustomer(t, server.URL+"/v1", 3, 1, "line-image-1", nil)
	a, _ := NewService(filepath.Join(t.TempDir(), "data"))
	a.LoadLicense(c.dir)
	in := request(imageLine(t, a, "image-1"))
	in.Count = 2
	a.Submit(in)
	a.Start(func() {})
	waitFor(t, "two images", func() bool { return countStatus(a, "succeeded") == 2 })
	a.LicenseStatus()
	a.Dispose()
	// Reinstalled, with a fresh data folder: the count is kept outside it.
	b, _ := NewService(filepath.Join(t.TempDir(), "data"))
	defer b.Dispose()
	b.LoadLicense(c.dir)
	if st := b.LicenseStatus(); st.TrialUsed != 2 || st.TrialRemaining != 1 {
		t.Fatalf("count reset by a new data folder: %+v", st)
	}
	// An edited record ends the trial.
	os.WriteFile(filepath.Join(licenseStateDir(), c.id+".bin"), []byte("edited"), 0o600)
	d, _ := NewService(filepath.Join(t.TempDir(), "data"))
	defer d.Dispose()
	d.LoadLicense(c.dir)
	if st := d.LicenseStatus(); st.TrialRemaining != 0 {
		t.Fatalf("tampered record kept the trial: %+v", st)
	}
}

func TestUpstreamQuotaShowsAndEndsTheTrial(t *testing.T) {
	isolateLicenseState(t)
	relay, server := newKeyRelay(t)
	s := testService(t)
	c := writeCustomer(t, server.URL+"/v1", 20, 1, "line-image-1", nil)
	s.LoadLicense(c.dir)
	s.LicenseStatus() // starts the background read
	waitFor(t, "upstream quota", func() bool { return s.LicenseStatus().UpstreamRemaining == "0.33 USD" })
	relay.mu.Lock()
	relay.remaining = 0
	relay.mu.Unlock()
	s.mu.Lock()
	s.lic.usage.at = s.lic.usage.at.Add(-2 * 60e9)
	s.mu.Unlock()
	s.LicenseStatus()
	waitFor(t, "quota spent", func() bool { return s.LicenseStatus().TrialRemaining == 0 })
	if _, e := s.Submit(request(imageLine(t, s, "image-1"))); e == nil || !strings.Contains(e.Error(), "试用额度已用完") {
		t.Fatal("submitted with the upstream quota spent", e)
	}
}

func TestUpstreamRefusalEndsTheTrial(t *testing.T) {
	isolateLicenseState(t)
	relay, server := newKeyRelay(t)
	relay.empty["line-image-1"] = true // quota spent at the upstream
	s := testService(t)
	c := writeCustomer(t, server.URL+"/v1", 20, 1, "line-image-1", nil)
	s.LoadLicense(c.dir)
	in := request(imageLine(t, s, "image-1"))
	in.Count = 2
	s.SetImageConcurrency(1)
	s.Submit(in)
	s.Start(func() {})
	waitFor(t, "paused", func() bool { return countStatus(s, "failed") == 1 && countStatus(s, "blocked") == 1 })
	failed := tasksWhere(s, func(t Task) bool { return t.Status == "failed" })[0]
	if !strings.HasPrefix(failed.Error, "试用额度已用完") || strings.Contains(failed.Error, "充值") {
		t.Fatal(failed.Error)
	}
	if relay.used("line-image-2") != 0 {
		t.Fatal("trial moved to a backup line")
	}
}

func TestActivationUnlocksBackupsAndKeepsData(t *testing.T) {
	isolateLicenseState(t)
	relay, server := newKeyRelay(t)
	s := testService(t)
	c := writeCustomer(t, server.URL+"/v1", 20, 1, "line-image-1", nil)
	s.LoadLicense(c.dir)
	line := imageLine(t, s, "image-1")
	s.Submit(request(line))
	s.Start(func() {})
	waitFor(t, "trial image", func() bool { return countStatus(s, "succeeded") == 1 })
	other, _ := license.NewCode()
	if _, e := s.Activate(other); e != license.ErrCode {
		t.Fatal("wrong code accepted", e)
	}
	if _, e := s.Activate("too-short"); e == nil {
		t.Fatal("malformed code accepted")
	}
	st, e := s.Activate(strings.ToLower(strings.ReplaceAll(c.code, "-", " ")))
	if e != nil || st.Mode != "active" || st.ActivatedAt == "" {
		t.Fatal(st, e)
	}
	g, _ := s.GetGatewaySettings()
	if g.PrimaryID != line.ID || len(g.BackupIDs) != 1 || g.BackupIDs[0] != imageLine(t, s, "image-2").ID {
		t.Fatalf("gateways: %+v", g)
	}
	if listed, _ := s.ListConnections(); len(listed) != 4 {
		t.Fatal("activation lists every line", len(listed))
	}
	in := request(line)
	in.Prompts, in.Count = []string{"a", "b"}, 11 // 22 images: no trial limit any more
	if _, e = s.Submit(in); e != nil {
		t.Fatal(e)
	}
	waitFor(t, "normal images", func() bool { return countStatus(s, "succeeded") == 23 })
	if relay.used("line-image-1") != 23 {
		t.Fatal("same key for trial and normal use", relay.used("line-image-1"))
	}
	if _, e = s.SaveConnection(ConnectionInput{ID: line.ID, Name: "x", BaseURL: server.URL + "/v1", Key: "mine"}); e == nil {
		t.Fatal("managed line edited")
	}
	if e = s.DeleteConnection(line.ID); e == nil {
		t.Fatal("managed line deleted")
	}
	cs, _ := s.ListConnections()
	raw, _ := json.Marshal(cs)
	logs, _ := s.ListLogs("", "", "", 0, 200)
	rawLogs, _ := json.Marshal(logs)
	for _, secret := range []string{"line-image-1", "line-llm-1", c.code, strings.ReplaceAll(c.code, "-", "")} {
		if strings.Contains(string(raw), secret) || strings.Contains(string(rawLogs), secret) {
			t.Fatal("secret exposed", secret)
		}
	}
}

func TestActivationSurvivesRestartNewDataAndUpgrade(t *testing.T) {
	isolateLicenseState(t)
	relay, server := newKeyRelay(t)
	root := filepath.Join(t.TempDir(), "data")
	s, _ := NewService(root)
	c := writeCustomer(t, server.URL+"/v1", 20, 1, "line-image-1", nil)
	s.LoadLicense(c.dir)
	if _, e := s.Activate(c.code); e != nil {
		t.Fatal(e)
	}
	s.Dispose()
	// An upgrade package replaced the key on image-1; same code.
	upgraded := writeCustomer(t, server.URL+"/v1", 20, 2, "line-image-1-new", &c)
	next, _ := NewService(root)
	defer next.Dispose()
	next.LoadLicense(upgraded.dir)
	if st := next.LicenseStatus(); st.Mode != "active" {
		t.Fatal("activation lost on upgrade", st)
	}
	next.Submit(request(imageLine(t, next, "image-1")))
	next.Start(func() {})
	waitFor(t, "new key used", func() bool { return relay.used("line-image-1-new") == 1 })
	// A fresh data folder on the same computer stays activated.
	fresh, _ := NewService(filepath.Join(t.TempDir(), "data"))
	defer fresh.Dispose()
	fresh.LoadLicense(upgraded.dir)
	if st := fresh.LicenseStatus(); st.Mode != "active" {
		t.Fatal("activation lost in a new data folder", st)
	}
}

func TestCustomerFileProblemsBlockGeneration(t *testing.T) {
	isolateLicenseState(t)
	_, server := newKeyRelay(t)
	s := testService(t)
	c := writeCustomer(t, server.URL+"/v1", 20, 1, "line-image-1", nil)
	data, _ := os.ReadFile(filepath.Join(c.dir, license.FileName))
	tampered := t.TempDir()
	os.WriteFile(filepath.Join(tampered, license.FileName), []byte(strings.Replace(string(data), `"payload": "`, `"payload": "e`, 1)), 0o600)
	s.LoadLicense(tampered)
	if st := s.LicenseStatus(); st.Mode != "invalid" {
		t.Fatal(st)
	}
	dev := saveConnection(t, s, server.URL+"/v1")
	if _, e := s.Submit(request(dev)); e == nil || !strings.Contains(e.Error(), "授权文件无效") {
		t.Fatal("tampered package generated", e)
	}
	root := filepath.Join(t.TempDir(), "data")
	a, _ := NewService(root)
	a.LoadLicense(c.dir)
	a.Dispose()
	other := writeCustomer(t, server.URL+"/v1", 20, 1, "line-image-1", nil)
	b, _ := NewService(root)
	defer b.Dispose()
	b.LoadLicense(other.dir)
	if st := b.LicenseStatus(); st.Mode != "mismatch" || !strings.Contains(st.Message, c.id) {
		t.Fatal(st)
	}
	if _, e := b.Activate(other.code); e == nil {
		t.Fatal("activated over another customer's data")
	}
}

func TestSub2APIKeyQuotaEndsTheTrial(t *testing.T) {
	isolateLicenseState(t)
	// 幻灵 and 1 Route answer an exhausted key with 429 API_KEY_QUOTA_EXHAUSTED.
	g := &fakeGateway{models: []string{"test-image"}, refuse: 429, refuseMessage: "API_KEY_QUOTA_EXHAUSTED"}
	server := newFakeGateway(t, g)
	s := testService(t)
	c := writeCustomer(t, server.URL+"/v1", 20, 1, "line-image-1", nil)
	s.LoadLicense(c.dir)
	in := request(imageLine(t, s, "image-1"))
	in.Count = 2
	s.SetImageConcurrency(1)
	s.Submit(in)
	s.Start(func() {})
	waitFor(t, "trial ended", func() bool { return countStatus(s, "failed") == 1 && countStatus(s, "blocked") == 1 })
	failed := tasksWhere(s, func(t Task) bool { return t.Status == "failed" })[0]
	if !strings.HasPrefix(failed.Error, "试用额度已用完（HTTP 429") || g.imageHits.Load() != 1 {
		t.Fatal(failed.Error, g.imageHits.Load())
	}
}
