package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"imageworkbench/internal/license"
)

// massMarket makes this process a mass-market build that trusts the key
// writeCustomer signed with: such builds check customer.dat with their update key
// and carry no licence key.
func massMarket(t *testing.T) {
	t.Helper()
	oldLine, oldUpdate, oldLicence := productLine, updatePublicKey, licensePublicKey
	productLine, updatePublicKey, licensePublicKey = "creative", licensePublicKey, ""
	t.Cleanup(func() { productLine, updatePublicKey, licensePublicKey = oldLine, oldUpdate, oldLicence })
}

func listed(t *testing.T, s *Service) string {
	t.Helper()
	cs, e := s.ListConnections()
	if e != nil {
		t.Fatal(e)
	}
	names := []string{}
	for _, c := range cs {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// One program, two editions (docs/adr/0003): without customer.dat the mass-market
// program is the open edition; importing a licence file makes it the hosted one,
// on the provider's lines only; removing the file gives the open edition back
// without leaving the provider's keys behind.
func TestMassMarketProgramIsOpenWithoutLicenceFileAndHostedWithIt(t *testing.T) {
	isolateLicenseState(t)
	_, relay := newKeyRelay(t)
	c := writeCustomer(t, relay.URL, 2, 1, "line-image-1", nil)
	massMarket(t)
	program := t.TempDir()
	s := testService(t)
	if e := s.LoadLicense(program); e != nil {
		t.Fatal(e)
	}
	// Open edition: no licence asked for, the user configures their own service.
	if st := s.LicenseStatus(); st.Mode != "developer" || st.Hosted || !st.CanImport {
		t.Fatalf("open edition: %+v", st)
	}
	own, e := s.SaveConnection(ConnectionInput{Name: "我自己的图片服务", BaseURL: relay.URL, Model: "test-image", Key: "my-own-key"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetGatewaySettings(GatewaySettings{PrimaryID: own.ID, AutoReroute: true}); e != nil {
		t.Fatal(e)
	}

	// The open edition's user buys the hosted edition: one file, no reinstall.
	st, e := s.ImportCustomerFile(filepath.Join(c.dir, license.FileName))
	if e != nil || st.Mode != "trial" || !st.Hosted || st.CanImport || st.TrialLimit != 2 {
		t.Fatalf("after importing the licence file: %+v %v", st, e)
	}
	if got := listed(t, s); got != "LLM 线路 1,图片线路 1" {
		t.Fatal("a trial of the hosted edition lists", got)
	}
	if g, _ := s.GetGatewaySettings(); g.PrimaryID != imageLine(t, s, "image-1").ID {
		t.Fatal("the provider's main image line is not in use", g)
	}
	if _, e = s.SaveConnection(ConnectionInput{Name: "再加一个", BaseURL: relay.URL, Model: "test-image", Key: "k"}); e == nil || !strings.Contains(e.Error(), "托管版") {
		t.Fatal("the hosted edition took a line of the user's own:", e)
	}
	s.mu.Lock()
	e = s.ensureCanGenerateLocked(1, own.ID)
	s.mu.Unlock()
	if e == nil || !strings.Contains(e.Error(), "托管版") {
		t.Fatal("the hosted edition generated on the user's own service:", e)
	}
	if st, e = s.Activate(c.code); e != nil || st.Mode != "active" {
		t.Fatal(st, e)
	}
	if got := listed(t, s); strings.Contains(got, "我自己的图片服务") || !strings.Contains(got, "图片线路 2") {
		t.Fatal("an activated hosted edition lists", got)
	}
	// Updates of the mass-market line never depend on the licence.
	s.mu.Lock()
	blocked := s.updateBlockedLocked()
	s.mu.Unlock()
	if blocked != "" {
		t.Fatal("update held back:", blocked)
	}

	// The provider's keys, as stored on this computer.
	secrets := []string{}
	s.mu.Lock()
	all, _ := List[Connection](s.store, "connection")
	for _, x := range all {
		if x.Managed != "" {
			secrets = append(secrets, filepath.Join(s.store.Root, "secrets", x.Credential+".bin"))
		}
	}
	s.mu.Unlock()
	if len(secrets) != 4 {
		t.Fatal("provider lines", len(secrets))
	}

	// The user deletes customer.dat: the open edition again, on their own service,
	// and nothing of the provider's lines is left to use.
	os.Remove(filepath.Join(program, license.FileName))
	if e = s.LoadLicense(program); e != nil {
		t.Fatal(e)
	}
	if st = s.LicenseStatus(); st.Mode != "developer" || st.Hosted || !st.CanImport {
		t.Fatalf("after removing the licence file: %+v", st)
	}
	if got := listed(t, s); got != "我自己的图片服务" {
		t.Fatal("the open edition lists", got)
	}
	for _, p := range secrets {
		if _, e = os.Stat(p); e == nil {
			t.Fatal("a provider key is still stored:", p)
		}
	}
	if g, _ := s.GetGatewaySettings(); g.PrimaryID != own.ID {
		t.Fatal("the user's own image service is not in use again", g)
	}
	if _, e = s.SaveConnection(ConnectionInput{Name: "第二个自己的服务", BaseURL: relay.URL, Model: "test-image", Key: "k2"}); e != nil {
		t.Fatal("the open edition refused a line:", e)
	}

	// Importing the same licence again: still activated, the trial is not reset.
	if st, e = s.ImportCustomerFile(filepath.Join(c.dir, license.FileName)); e != nil || st.Mode != "active" || !st.Hosted {
		t.Fatalf("after importing again: %+v %v", st, e)
	}
	// And a licence for another person replaces it only through the provider:
	// this program now belongs to c.
	other := writeCustomerSigned(t, relay.URL, c)
	if _, e = s.ImportCustomerFile(filepath.Join(other, license.FileName)); e == nil || !strings.Contains(e.Error(), "另一份软件") {
		t.Fatal("a licence of another software ID replaced this one:", e)
	}
}

// writeCustomerSigned signs a customer.dat for a new software ID with c's key.
func writeCustomerSigned(t *testing.T, base string, c testCustomer) string {
	t.Helper()
	id, _ := license.NewSoftwareID()
	code, _ := license.NewCode()
	check, _ := license.NewCodeCheck(code, id)
	conns := []license.Connection{
		{Slot: "image-1", Kind: "image", Name: "图片线路 1", BaseURL: base, Model: "test-image"},
		{Slot: "image-2", Kind: "image", Name: "图片线路 2", BaseURL: base, Model: "test-image"},
		{Slot: "llm-1", Kind: "llm", Name: "LLM 线路 1", BaseURL: base, Model: "m", Verified: true},
	}
	for i := range conns {
		conns[i].KeyObf, _ = license.ObfuscateKey("k", id)
	}
	data, e := license.Sign(license.Profile{SoftwareID: id, Customer: "顾客 B", TrialImages: 1, Connections: conns, ImageBackups: []string{"image-2"}, LinesRevision: 1, CodeCheck: check}, c.priv)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, license.FileName), data, 0o600)
	return dir
}

// A licence file that is there but not valid locks the program and says how to
// go back to the open edition; an expired one only asks for renewal. Neither
// falls back to the open edition by itself.
func TestMassMarketInvalidOrExpiredLicenceDoesNotBecomeTheOpenEdition(t *testing.T) {
	isolateLicenseState(t)
	_, relay := newKeyRelay(t)
	testExpiresAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	t.Cleanup(func() { testExpiresAt = "" })
	c := writeCustomer(t, relay.URL, 2, 1, "line-image-1", nil)
	massMarket(t)

	s := testService(t)
	if e := s.LoadLicense(c.dir); e != nil {
		t.Fatal(e)
	}
	st := s.LicenseStatus()
	if st.Mode != "expired" || !st.Hosted || st.CanImport || !strings.Contains(st.Message, "续费") || strings.Contains(st.Message, "删除") {
		t.Fatalf("expired hosted edition: %+v", st)
	}
	s.mu.Lock()
	e := s.ensureCanGenerateLocked(1)
	s.mu.Unlock()
	if e == nil {
		t.Fatal("an expired hosted edition generates")
	}
	if _, e = s.SaveConnection(ConnectionInput{Name: "自己的", BaseURL: relay.URL, Model: "test-image", Key: "k"}); e == nil {
		t.Fatal("an expired hosted edition took the user's own line")
	}

	// Tampered file.
	dir := t.TempDir()
	data, _ := os.ReadFile(filepath.Join(c.dir, license.FileName))
	os.WriteFile(filepath.Join(dir, license.FileName), append(data[:len(data)-8], []byte("tampered")...), 0o600)
	s2 := testService(t)
	if e = s2.LoadLicense(dir); e != nil {
		t.Fatal(e)
	}
	if st = s2.LicenseStatus(); st.Mode != "invalid" || !strings.Contains(st.Message, "删除程序目录里的 customer.dat") {
		t.Fatalf("tampered licence file: %+v", st)
	}
	if _, e = s2.SaveConnection(ConnectionInput{Name: "自己的", BaseURL: relay.URL, Model: "test-image", Key: "k"}); e == nil {
		t.Fatal("a program with an invalid licence file took the user's own line")
	}
}
