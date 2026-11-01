package admin

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"imageworkbench/internal/core"
	"imageworkbench/internal/license"
)

func open(t *testing.T) *Admin {
	t.Helper()
	a, e := Open(filepath.Join(t.TempDir(), "admin"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.EnsureSigningKey(); e != nil {
		t.Fatal(e)
	}
	return a
}

type pool struct {
	imageA, imageB, llm Upstream
}

// stock adds upstreams and enough keys for n customers; LLM keys are marked
// verified directly (the real check makes billable calls).
func stock(t *testing.T, a *Admin, n int, base string) pool {
	t.Helper()
	if base == "" {
		base = "https://relay.example.test/v1"
	}
	ia, _ := a.SaveUpstream(Upstream{Name: "幻灵", Kind: "image", BaseURL: base, Model: "test-image"})
	ib, _ := a.SaveUpstream(Upstream{Name: "1 Route", Kind: "image", BaseURL: base, Model: "test-image"})
	l, _ := a.SaveUpstream(Upstream{Name: "LLM", Kind: "llm", BaseURL: base, Model: "m"})
	keys := func(prefix string, count int) string {
		lines := []string{}
		for i := 0; i < count; i++ {
			lines = append(lines, fmt.Sprintf("sk-%s-%02d-secret", prefix, i))
		}
		return strings.Join(lines, "\n")
	}
	must := func(_ ImportResult, e error) {
		if e != nil {
			t.Fatal(e)
		}
	}
	must(a.ImportKeys(ia.ID, "primary", keys("prim-a", 2*n), ""))
	must(a.ImportKeys(ib.ID, "primary", keys("prim-b", n), ""))
	must(a.ImportKeys(ib.ID, "backup", keys("back-b", 3*n), ""))
	must(a.ImportKeys(l.ID, "primary", keys("llm-prim", n), ""))
	must(a.ImportKeys(l.ID, "backup", keys("llm-back", n), ""))
	a.mu.Lock()
	for i := range a.d.Keys {
		if a.d.Keys[i].Kind == "llm" {
			a.d.Keys[i].Verified = true
		}
	}
	a.mu.Unlock()
	return pool{ia, ib, l}
}

func TestImportDeduplicatesAndAssignmentNeverRepeats(t *testing.T) {
	a := open(t)
	p := stock(t, a, 1, "")
	r, e := a.ImportKeys(p.imageA.ID, "primary", "sk-prim-a-00-secret\nsk-prim-a-00-secret\nsk-new-key-0001\nshort", "")
	if e != nil || r.Added != 1 || r.Duplicates != 2 || r.Invalid != 1 {
		t.Fatal(r, e)
	}
	c1, _ := a.CreateCustomer("客户 A", 0)
	c2, _ := a.CreateCustomer("客户 B", 0)
	if c1.TrialImages != 20 || c1.SoftwareID == c2.SoftwareID {
		t.Fatal(c1, c2)
	}
	if _, e = a.CreateCustomer("客户 A", 0); e == nil {
		t.Fatal("duplicate customer name accepted")
	}
	missing, e := a.AutoAssign(c1.ID)
	if e != nil || len(missing) != 0 {
		t.Fatal(missing, e)
	}
	st := a.State()
	got := st.Customers[0].Slots
	if len(got) != 8 {
		t.Fatal("slots", got)
	}
	// Image primaries come from both image upstreams.
	ups := map[string]bool{}
	for _, slot := range []string{"image-1", "image-2", "image-3"} {
		for _, k := range st.Keys {
			if k.ID == got[slot] {
				ups[k.UpstreamID] = true
			}
		}
	}
	if len(ups) != 2 {
		t.Fatal("image primaries all from one upstream")
	}
	// The second customer gets only what is left, never a key of the first.
	missing, _ = a.AutoAssign(c2.ID)
	if len(missing) == 0 {
		t.Fatal("expected missing slots for the second customer")
	}
	used := map[string]string{}
	for _, c := range a.State().Customers {
		for slot, id := range c.Slots {
			if other, dup := used[id]; dup {
				t.Fatalf("key on %s and %s", other, slot)
			}
			used[id] = c.Name + "/" + slot
		}
	}
	var otherKey string
	for _, k := range a.State().Keys {
		if k.CustomerID == c1.ID && k.Slot == "image-1" {
			otherKey = k.ID
		}
	}
	if e = a.AssignKey(c2.ID, "image-1", otherKey); e == nil {
		t.Fatal("assigned another customer's key")
	}
	if e = a.AssignKey(c1.ID, "llm-1", got["image-2"]); e == nil {
		t.Fatal("image key accepted on an LLM line")
	}
}

func TestStateAndLedgerHoldNoSecrets(t *testing.T) {
	a := open(t)
	stock(t, a, 1, "")
	c, _ := a.CreateCustomer("客户 A", 0)
	a.AutoAssign(c.ID)
	code, _ := a.RevealCode(c.ID)
	raw, _ := json.Marshal(a.State())
	ledger, _ := os.ReadFile(a.ledgerPath())
	for _, text := range []string{string(raw), string(ledger)} {
		if strings.Contains(text, "-secret") || strings.Contains(text, code) {
			t.Fatal("secret in state or ledger")
		}
	}
	if len(code) != 29 {
		t.Fatal("code", code)
	}
}

// fakeBuilder writes the program files a real build would produce.
type fakeBuilder struct{ leak string }

func (f fakeBuilder) Build(_ context.Context, b BuildRequest, log func(string)) error {
	dir := filepath.Join(b.RepoRoot, b.ReleaseDir)
	files := map[string]string{"ImageWorkbench.exe": "exe", "product-browser.exe": "pb", "runtime/msedgewebview2.exe": "rt", "agent/claude-code/claude.exe": "agent", "使用说明.txt": "help " + f.leak, "version.json": "{}"}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if e := os.WriteFile(p, []byte(body), 0o644); e != nil {
			return e
		}
	}
	log("fake build done")
	return nil
}

func repo(t *testing.T, a *Admin) string {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "scripts"), 0o755)
	os.WriteFile(filepath.Join(dir, "scripts", "build.ps1"), []byte("#"), 0o644)
	if e := a.SetRepoRoot(dir); e != nil {
		t.Fatal(e)
	}
	return dir
}

func TestPackageRefusesIncompleteOrUnverifiedLines(t *testing.T) {
	a := open(t)
	c, _ := a.CreateCustomer("客户 A", 0)
	repo(t, a)
	if _, e := a.BuildPackage(context.Background(), c.ID, "0.6.2", fakeBuilder{}, "", func(string) {}); e == nil || !strings.Contains(e.Error(), "还没有分配 Key") {
		t.Fatal(e)
	}
	stock(t, a, 1, "")
	a.AutoAssign(c.ID)
	a.mu.Lock()
	for i := range a.d.Keys {
		a.d.Keys[i].Verified = false
	}
	a.mu.Unlock()
	if _, _, e := a.CustomerProfile(c.ID); e == nil || !strings.Contains(e.Error(), "能力验证") {
		t.Fatal("unverified LLM line packaged", e)
	}
}

// Only image line 1 and LLM line 1 are required; empty optional lines stay out
// of customer.dat and out of the backup order.
func TestPackageNeedsOnlyMainLines(t *testing.T) {
	a := open(t)
	p := stock(t, a, 1, "")
	c, _ := a.CreateCustomer("客户 B", 0)
	repo(t, a)
	assign := func(slot, upstreamID, use string) {
		t.Helper()
		for _, k := range a.State().Keys {
			if k.UpstreamID == upstreamID && k.Use == use && k.Status == "available" {
				if e := a.AssignKey(c.ID, slot, k.ID); e != nil {
					t.Fatal(e)
				}
				return
			}
		}
		t.Fatal("no key for", slot)
	}
	assign("image-1", p.imageA.ID, "primary")
	if _, _, e := a.CustomerProfile(c.ID); e == nil || !strings.Contains(e.Error(), "LLM 线路 1 还没有分配 Key") {
		t.Fatal("packaged without LLM line 1", e)
	}
	// Any free key of the right kind fits any line: a backup-labelled LLM key on LLM line 1.
	assign("llm-1", p.llm.ID, "backup")
	if _, _, e := a.CustomerProfile(c.ID); e == nil || !strings.Contains(e.Error(), "至少还要分配一条备用图片线路") {
		t.Fatal("packaged without a backup image line", e)
	}
	assign("image-4", p.imageB.ID, "backup")
	data, _, e := a.CustomerProfile(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	prof, e := license.Verify(data, []string{a.State().SigningPublicKey})
	if e != nil {
		t.Fatal(e)
	}
	slots := []string{}
	for _, conn := range prof.Connections {
		slots = append(slots, conn.Slot)
	}
	if strings.Join(slots, ",") != "image-1,image-4,llm-1" || strings.Join(prof.ImageBackups, ",") != "image-4" {
		t.Fatalf("lines %v, backups %v", slots, prof.ImageBackups)
	}
	if got := a.State().Customers[0].ImageBackups; len(got) != len(defaultOrder) {
		t.Fatal("saved backup order lost its empty lines", got)
	}
	if _, e = a.BuildPackage(context.Background(), c.ID, "0.6.6", fakeBuilder{}, "", func(string) {}); e != nil {
		t.Fatal(e)
	}
}

// Deleting a customer frees keys that never left this computer and disables the
// delivered ones, so a key in someone's software is never handed out again.
func TestDeleteCustomerFreesUndeliveredAndDisablesDeliveredKeys(t *testing.T) {
	a := open(t)
	stock(t, a, 1, "")
	repo(t, a)
	c, _ := a.CreateCustomer("客户 C", 0)
	a.AutoAssign(c.ID)
	if _, e := a.BuildPackage(context.Background(), c.ID, "0.6.15", fakeBuilder{}, "", func(string) {}); e != nil {
		t.Fatal(e)
	}
	r, _ := a.ImportKeys(a.State().Upstreams[0].ID, "primary", "sk-late-key-0001", "")
	if r.Added != 1 {
		t.Fatal(r)
	}
	var late, delivered string
	for _, k := range a.State().Keys {
		if k.Tail == "0001" {
			late = k.ID
		}
		if k.Slot == "image-1" {
			delivered = k.ID
		}
	}
	if e := a.AssignKey(c.ID, "image-1", late); e != nil {
		t.Fatal(e)
	}
	a.mu.Lock()
	cust, _ := a.customerLocked(c.ID)
	codeFile := filepath.Join(a.root, "secrets", cust.CodeRef+".bin")
	a.mu.Unlock()
	if e := a.DeleteCustomer(c.ID); e != nil {
		t.Fatal(e)
	}
	st := a.State()
	if len(st.Customers) != 0 {
		t.Fatal("customer still listed")
	}
	for _, k := range st.Keys {
		if k.CustomerID != "" || k.Slot != "" {
			t.Fatal("key still points at the deleted customer", k)
		}
		if k.ID == late && k.Status != "available" {
			t.Fatal("undelivered key not returned to the pool", k.Status)
		}
		if (k.ID == delivered || k.Delivered) && k.Status != "disabled" {
			t.Fatal("delivered key can be handed out again", k)
		}
	}
	if _, e := os.Stat(codeFile); !os.IsNotExist(e) {
		t.Fatal("activation code file left behind", e)
	}
	if e := a.DeleteCustomer(c.ID); e == nil {
		t.Fatal("deleted a missing customer")
	}

	// The provider may re-enable a delivered key; it can be assigned again, keeps
	// the "delivered before" mark, and a new undelivered assignment returns to the pool.
	if e := a.EnableKey(late); e == nil {
		t.Fatal("re-enabled a key that was not disabled")
	}
	if e := a.EnableKey(delivered); e != nil {
		t.Fatal(e)
	}
	c2, _ := a.CreateCustomer("客户 D", 0)
	if e := a.AssignKey(c2.ID, "image-1", delivered); e != nil {
		t.Fatal(e)
	}
	if e := a.UnassignKey(c2.ID, "image-1"); e != nil {
		t.Fatal(e)
	}
	for _, k := range a.State().Keys {
		if k.ID == delivered && (k.Status != "available" || !k.DeliveredBefore || k.Delivered || k.DisabledFor != "") {
			t.Fatal("re-enabled key in the wrong state", k)
		}
	}
}

// Bulk actions are all-or-nothing: a delivered or assigned key in the selection
// stops a delete, and an assigned one stops a disable.
func TestBulkDeleteAndDisableKeys(t *testing.T) {
	a := open(t)
	stock(t, a, 1, "")
	repo(t, a)
	c, _ := a.CreateCustomer("客户 E", 0)
	a.AutoAssign(c.ID)
	if _, e := a.BuildPackage(context.Background(), c.ID, "0.6.17", fakeBuilder{}, "", func(string) {}); e != nil {
		t.Fatal(e)
	}
	u := a.State().Upstreams[0].ID
	if r, _ := a.ImportKeys(u, "primary", "sk-spare-key-0001\nsk-spare-key-0002\nsk-spare-key-0003", ""); r.Added != 3 {
		t.Fatal(r)
	}
	var spare []string
	var delivered string
	for _, k := range a.State().Keys {
		switch {
		case strings.HasPrefix(k.Tail, "000"):
			spare = append(spare, k.ID)
		case k.Slot == "image-1":
			delivered = k.ID
		}
	}
	// Swap image-1 so its delivered key is disabled and a spare is assigned.
	if e := a.AssignKey(c.ID, "image-1", spare[2]); e != nil {
		t.Fatal(e)
	}
	if e := a.DeleteKeys(append(slices.Clone(spare[:2]), delivered)); e == nil || !strings.Contains(e.Error(), "交付过") {
		t.Fatal("deleted a delivered key", e)
	}
	if e := a.DeleteKeys([]string{spare[0], spare[2]}); e == nil {
		t.Fatal("deleted an assigned key")
	}
	if e := a.DisableKeys([]string{spare[0], spare[2]}, "批量停用"); e == nil {
		t.Fatal("disabled an assigned key")
	}
	if e := a.DisableKeys([]string{spare[1], delivered}, "批量停用"); e != nil {
		t.Fatal(e)
	}
	var refs []string
	a.mu.Lock()
	for _, id := range spare[:2] {
		k, _ := a.keyLocked(id)
		refs = append(refs, filepath.Join(a.root, "secrets", k.SecretRef+".bin"))
	}
	a.mu.Unlock()
	if e := a.DeleteKeys(spare[:2]); e != nil {
		t.Fatal(e)
	}
	for _, k := range a.State().Keys {
		if k.ID == spare[0] || k.ID == spare[1] {
			t.Fatal("key still listed", k)
		}
		if k.ID == delivered && (k.Status != "disabled" || k.DisabledFor == "批量停用") {
			t.Fatal("delivered key lost its original reason", k)
		}
		if k.ID == spare[2] && k.Status != "assigned" {
			t.Fatal("assigned key touched by a failed bulk action", k)
		}
	}
	for _, f := range refs {
		if _, e := os.Stat(f); !os.IsNotExist(e) {
			t.Fatal("secret file left behind", f)
		}
	}
	if e := a.DeleteKeys(nil); e == nil {
		t.Fatal("empty selection accepted")
	}
}

func TestBuildPackageZipsChecksAndRecords(t *testing.T) {
	a := open(t)
	stock(t, a, 1, "")
	c, _ := a.CreateCustomer("客户:A", 0)
	a.AutoAssign(c.ID)
	dir := repo(t, a)
	var logs []string
	d, e := a.BuildPackage(context.Background(), c.ID, "0.6.2", fakeBuilder{}, "", func(s string) { logs = append(logs, s) })
	if e != nil {
		t.Fatal(e, logs)
	}
	if filepath.Base(d.Zip) != "ImageWorkbench-客户_A-0.6.2.zip" || d.VaultRevision != 1 || len(d.SHA256) != 64 {
		t.Fatalf("delivery: %+v", d)
	}
	z, e := zip.OpenReader(d.Zip)
	if e != nil {
		t.Fatal(e)
	}
	names := map[string]bool{}
	for _, f := range z.File {
		names[f.Name] = true
	}
	z.Close()
	for _, want := range []string{"customer.dat", "ImageWorkbench.exe", "runtime/msedgewebview2.exe", "agent/claude-code/claude.exe", "使用说明.txt"} {
		if !names[want] {
			t.Fatal("missing", want, names)
		}
	}
	if _, e = os.Stat(filepath.Join(dir, "release", "customers", c.SoftwareID, "0.6.2", "customer.dat")); e != nil {
		t.Fatal("release folder lacks customer.dat")
	}
	st := a.State().Customers[0]
	if st.Changed || st.VaultRevision != 1 || len(st.Deliveries) != 1 {
		t.Fatalf("%+v", st)
	}
	// Unchanged lines keep the revision; a replaced key raises it and retires the old key.
	if _, rev, _ := a.CustomerProfile(c.ID); rev != 1 {
		t.Fatal("revision changed without a change", rev)
	}
	r, _ := a.ImportKeys(a.State().Upstreams[0].ID, "primary", "sk-replacement-key-01", "")
	if r.Added != 1 {
		t.Fatal(r)
	}
	var replacement, old string
	for _, k := range a.State().Keys {
		if k.Tail == "y-01" && k.Status == "available" {
			replacement = k.ID
		}
		if k.Slot == "image-1" {
			old = k.ID
		}
	}
	if e = a.AssignKey(c.ID, "image-1", replacement); e != nil {
		t.Fatal(e)
	}
	for _, k := range a.State().Keys {
		if k.ID == old && k.Status != "disabled" {
			t.Fatal("a delivered key went back to the pool", k.Status)
		}
	}
	if _, rev, _ := a.CustomerProfile(c.ID); rev != 2 {
		t.Fatal("key change must raise the vault revision", rev)
	}

	// A plain key anywhere in the package fails the check and leaves no ZIP.
	a.mu.Lock()
	cust, _ := a.customerLocked(c.ID)
	k, _ := a.keyLocked(cust.Slots["llm-1"])
	leak, _ := a.secret(k.SecretRef)
	a.mu.Unlock()
	_, e = a.BuildPackage(context.Background(), c.ID, "0.6.3", fakeBuilder{leak: string(leak)}, "", func(string) {})
	if e == nil || !strings.Contains(e.Error(), "LLM 线路 1 Key found in 使用说明.txt") || strings.Contains(e.Error(), string(leak)) {
		t.Fatal(e)
	}
	if _, err := os.Stat(filepath.Join(dir, "release", "customers", c.SoftwareID, zipName(c.Name, "0.6.3"))); err == nil {
		t.Fatal("failed package left behind")
	}
}

func TestSigningKeyBackupRoundTrip(t *testing.T) {
	a := open(t)
	pub := a.State().SigningPublicKey
	path := filepath.Join(t.TempDir(), "signing-backup.json")
	if e := a.ExportSigningBackup(path, "short"); e == nil {
		t.Fatal("weak password accepted")
	}
	if e := a.ExportSigningBackup(path, "a long backup password"); e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(path)
	a.mu.Lock()
	priv, _ := a.signingKeyLocked()
	a.mu.Unlock()
	if strings.Contains(string(raw), base64.StdEncoding.EncodeToString(priv)) {
		t.Fatal("private key in clear")
	}
	other := open(t) // a new computer: its own fresh key
	if e := other.ImportSigningBackup(path, "a long backup password", false); e == nil {
		t.Fatal("replaced a different key without confirmation")
	}
	if e := other.ImportSigningBackup(path, "wrong password!!", true); e == nil {
		t.Fatal("wrong password accepted")
	}
	if e := other.ImportSigningBackup(path, "a long backup password", true); e != nil || other.State().SigningPublicKey != pub {
		t.Fatal(e)
	}
}

// The real app trusts the admin key, runs the trial on the main line and, after
// the activation code, uses the customer's own keys.
func TestPackageActivatesInTheApp(t *testing.T) {
	isolate(t)
	var mu sync.Mutex
	used := []string{}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "test-image"}}})
			return
		}
		mu.Lock()
		used = append(used, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		mu.Unlock()
		png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": png}}})
	}))
	defer relay.Close()
	a := open(t)
	stock(t, a, 1, relay.URL+"/v1")
	c, _ := a.CreateCustomer("客户 A", 2)
	a.AutoAssign(c.ID)
	data, _, e := a.CustomerProfile(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer core.TrustLicenseKeyForTest(a.State().SigningPublicKey)()
	pkg := t.TempDir()
	os.WriteFile(filepath.Join(pkg, license.FileName), data, 0o600)
	s, e := core.NewService(filepath.Join(t.TempDir(), "data"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Dispose()
	s.LoadLicense(pkg)
	st := s.LicenseStatus()
	if st.Mode != "trial" || st.SoftwareID != c.SoftwareID || st.TrialLimit != 2 {
		t.Fatalf("%+v", st)
	}
	// An LLM line without a configured limit reaches the customer as 32768, not 8192.
	cs, _ := s.ListConnections()
	llmLines := 0
	for _, x := range cs {
		if x.Kind == "llm" {
			llmLines++
			if x.MaxOutputTokens != core.DefaultMaxOutputTokens {
				t.Fatalf("customer LLM line limit %d", x.MaxOutputTokens)
			}
		}
	}
	if llmLines == 0 {
		t.Fatal("package has no LLM line")
	}
	lineID := func() string {
		cs, _ := s.ListConnections()
		for _, x := range cs {
			if x.Managed == "image-1" {
				return x.ID
			}
		}
		return ""
	}
	submit := func() error {
		_, e := s.Submit(core.GenerateInput{RequestID: core.NewID(), ConnectionID: lineID(), Model: "test-image", Prompts: []string{"p"}, Count: 1, References: []string{}, Size: "auto", Quality: "auto"})
		return e
	}
	s.Start(func() {})
	if e = submit(); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(used) == 1 })
	code, _ := a.RevealCode(c.ID)
	if _, e = s.Activate(code); e != nil {
		t.Fatal(e)
	}
	if e = submit(); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(used) == 2 })
	mu.Lock()
	defer mu.Unlock()
	if !strings.HasPrefix(used[0], "sk-prim-") || used[1] != used[0] {
		t.Fatal("keys used", used)
	}
}

func wait(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestEmptyStateUsesEmptyLists(t *testing.T) {
	a := open(t)
	raw, _ := json.Marshal(a.State())
	if strings.Contains(string(raw), "null") {
		t.Fatal("null list in state:", string(raw))
	}
}

func TestUpstreamQuotaFlagAndConsoleURL(t *testing.T) {
	a := open(t)
	if _, e := a.SaveUpstream(Upstream{Name: "X", Kind: "llm", BaseURL: "https://x.test/v1", Model: "m", ConsoleURL: "javascript:alert(1)"}); e == nil {
		t.Fatal("non-web console URL accepted")
	}
	u, e := a.SaveUpstream(Upstream{Name: "DeepSeek 官方", Kind: "llm", BaseURL: "https://api.deepseek.com/anthropic", Model: "deepseek-flash", ConsoleURL: "https://platform.deepseek.com/"})
	if e != nil || u.PerKeyQuota {
		t.Fatal(u, e)
	}
	s, _ := a.SaveUpstream(Upstream{Name: "幻灵", Kind: "image", BaseURL: "https://ai.huanling.icu/v1", Model: "gpt-image-2", PerKeyQuota: true})
	reopened, _ := Open(a.Root())
	for _, x := range reopened.State().Upstreams {
		if x.ID == s.ID && !x.PerKeyQuota {
			t.Fatal("flag lost")
		}
		if x.ID == u.ID && x.ConsoleURL != "https://platform.deepseek.com/" {
			t.Fatal("console URL lost")
		}
	}
}

func TestPaymentStepAndUsageAddress(t *testing.T) {
	a := open(t)
	u, e := a.SaveUpstream(Upstream{Name: "幻灵", Kind: "image", BaseURL: "https://ai.huanling.icu/v1", Model: "gpt-image-2", PerKeyQuota: true})
	if e != nil || u.UsageURL != "https://ai.huanling.icu/v1/usage" {
		t.Fatal("default usage address", u.UsageURL, e)
	}
	if _, e = a.ImportKeys(u.ID, "trial", "sk-old-style-trial-key", ""); e == nil {
		t.Fatal("trial keys are no longer a separate use")
	}
	c, _ := a.CreateCustomer("客户 A", 0)
	if a.State().Customers[0].Paid {
		t.Fatal("new customer marked paid")
	}
	if e = a.MarkPaid(c.ID, "年付，额度 $50"); e != nil {
		t.Fatal(e)
	}
	got := a.State().Customers[0]
	last := got.Events[len(got.Events)-1]
	if !got.Paid || got.PaidAt == "" || last.Kind != "paid" || !strings.Contains(last.Text, "调高") {
		t.Fatalf("%+v", got)
	}
}

func TestKeyUseChangeUpstreamDeletionAndModelChange(t *testing.T) {
	a := open(t)
	p := stock(t, a, 1, "")
	var llmKey PoolKey
	for _, k := range a.State().Keys {
		if k.Kind == "llm" && k.Use == "primary" {
			llmKey = k
		}
	}
	// A key imported with the wrong role can be switched while unassigned.
	if e := a.SetKeyUse(llmKey.ID, "backup"); e != nil {
		t.Fatal(e)
	}
	if e := a.SetKeyUse(llmKey.ID, "trial"); e == nil {
		t.Fatal("unknown role accepted")
	}
	k, _ := a.keyLocked(llmKey.ID)
	if k.Use != "backup" {
		t.Fatal("role not saved", k.Use)
	}
	a.SetKeyUse(llmKey.ID, "primary")
	// Changing the LLM model drops the verification of its keys.
	p.llm.Model = "other-model"
	if _, e := a.SaveUpstream(p.llm); e != nil {
		t.Fatal(e)
	}
	for _, k := range a.State().Keys {
		if k.Kind == "llm" && k.Verified {
			t.Fatal("verification kept after the model changed")
		}
	}
	// An upstream whose keys were never handed out goes away with its keys.
	unused, _ := a.SaveUpstream(Upstream{Name: "unused", Kind: "llm", BaseURL: "https://x.test/v1", Model: "m"})
	a.ImportKeys(unused.ID, "primary", "sk-unused-0001\nsk-unused-0002", "")
	ids := []string{}
	for _, k := range a.State().Keys {
		if k.UpstreamID == unused.ID {
			ids = append(ids, k.ID)
		}
	}
	a.DisableKey(ids[0], "test")
	secrets, _ := os.ReadDir(filepath.Join(a.Root(), "secrets"))
	if e := a.DeleteUpstream(unused.ID); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadDir(filepath.Join(a.Root(), "secrets"))
	if len(after) != len(secrets)-2 {
		t.Fatal("key secrets left behind", len(secrets), len(after))
	}
	for _, k := range a.State().Keys {
		if k.UpstreamID == unused.ID {
			t.Fatal("key kept after its upstream was deleted")
		}
	}
	// Keys that a customer holds keep their upstream.
	c, _ := a.CreateCustomer("客户", 20)
	if _, e := a.AutoAssign(c.ID); e != nil {
		t.Fatal(e)
	}
	if e := a.DeleteUpstream(p.imageA.ID); e == nil {
		t.Fatal("upstream with assigned keys deleted")
	}
	held, _ := a.keyLocked(a.State().Customers[0].Slots["image-1"])
	if e := a.SetKeyUse(held.ID, "backup"); e == nil {
		t.Fatal("role of an assigned key changed")
	}
}
