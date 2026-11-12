package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"imageworkbench/internal/license"
)

func withExpiry(t *testing.T, at time.Time) {
	t.Helper()
	old := testExpiresAt
	testExpiresAt = at.UTC().Format(time.RFC3339)
	t.Cleanup(func() { testExpiresAt = old })
}

func TestExpiredLicenceStopsNewImagesOnly(t *testing.T) {
	isolateLicenseState(t)
	_, server := newKeyRelay(t)
	s := testService(t)
	withExpiry(t, time.Now().Add(-time.Hour))
	c := writeCustomer(t, server.URL+"/v1", 5, 1, "line-image-1", nil)
	if e := s.LoadLicense(c.dir); e != nil {
		t.Fatal(e)
	}
	st := s.LicenseStatus()
	if st.Mode != "expired" || !strings.Contains(st.Message, "到期") || st.ExpiresAt == "" {
		t.Fatalf("status %+v", st)
	}
	in := request(imageLine(t, s, "image-1"))
	if _, e := s.Submit(in); e == nil || !strings.Contains(e.Error(), "到期") {
		t.Fatal("expired licence generated", e)
	}
	// An image queued before it ended is held back (never sent) with the reason.
	task := Task{ID: NewID(), BatchID: "b", CreatedAt: Now(), UpdatedAt: Now(), Status: "queued", Connection: imageLine(t, s, "image-1"), Model: "test-image", References: []string{}}
	s.store.Put("task", task.ID, task)
	s.claimTask(false)
	var after Task
	s.store.Get("task", task.ID, &after)
	if after.Status != "blocked" || !strings.Contains(after.Error, "到期") {
		t.Fatalf("queued image %s %s", after.Status, after.Error)
	}
	// Viewing and exporting still work.
	if _, e := s.ListTasks("", 0, 10); e != nil {
		t.Fatal(e)
	}
}

func TestExpiryWarningAndClockRollback(t *testing.T) {
	isolateLicenseState(t)
	_, server := newKeyRelay(t)
	s := testService(t)
	withExpiry(t, time.Now().Add(60*time.Hour))
	c := writeCustomer(t, server.URL+"/v1", 5, 1, "line-image-1", nil)
	if e := s.LoadLicense(c.dir); e != nil {
		t.Fatal(e)
	}
	if st := s.LicenseStatus(); st.Mode != "trial" || st.DaysLeft != 3 {
		t.Fatalf("warning %+v", st)
	}
	// The software was last seen two hours ahead of this clock: set back.
	s.mu.Lock()
	s.lic.shared.LastSeen = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	saveShared(s.lic.profile.SoftwareID, s.lic.shared)
	s.mu.Unlock()
	// Restarting does not forget it: the time is kept outside the data folder.
	s2 := testService(t)
	if e := s2.LoadLicense(c.dir); e != nil {
		t.Fatal(e)
	}
	if st := s2.LicenseStatus(); st.Mode != "expired" || !strings.Contains(st.Message, "系统时间") {
		t.Fatalf("rollback %+v", st)
	}
}

func TestNoExpiryKeepsFormat2Behaviour(t *testing.T) {
	isolateLicenseState(t)
	_, server := newKeyRelay(t)
	s := testService(t)
	c := writeCustomer(t, server.URL+"/v1", 5, 1, "line-image-1", nil)
	data, _ := os.ReadFile(filepath.Join(c.dir, license.FileName))
	if !strings.Contains(string(data), `"format": 2`) {
		t.Fatal("old-style package not format 2")
	}
	s.LoadLicense(c.dir)
	if st := s.LicenseStatus(); st.Mode != "trial" || st.ExpiresAt != "" || st.DaysLeft != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestRenewalImportReplacesCustomerFile(t *testing.T) {
	isolateLicenseState(t)
	_, server := newKeyRelay(t)
	s := testService(t)
	withExpiry(t, time.Now().Add(-time.Hour))
	c := writeCustomer(t, server.URL+"/v1", 5, 1, "line-image-1", nil)
	if e := s.LoadLicense(c.dir); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Activate(c.code); e != nil {
		t.Fatal(e)
	}
	if s.LicenseStatus().Mode != "expired" {
		t.Fatal("not expired")
	}
	// The provider renews: same software, new expiry, signed with the same key.
	testExpiresAt = time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)
	renewed := writeCustomer(t, server.URL+"/v1", 5, 1, "line-image-1", &c)
	st, e := s.ImportCustomerFile(filepath.Join(renewed.dir, license.FileName))
	if e != nil || st.Mode != "active" || st.ExpiresAt != testExpiresAt {
		t.Fatalf("renewal %+v %v", st, e)
	}
	got, _ := os.ReadFile(filepath.Join(c.dir, license.FileName))
	want, _ := os.ReadFile(filepath.Join(renewed.dir, license.FileName))
	if string(got) != string(want) {
		t.Fatal("customer.dat not replaced")
	}
	if _, e = os.Stat(filepath.Join(c.dir, license.FileName+".bak")); e != nil {
		t.Fatal("old file not kept", e)
	}
	if _, e = s.Submit(request(imageLine(t, s, "image-1"))); e != nil {
		t.Fatal("renewed licence still blocked", e)
	}

	// Another software's file, a tampered file, or a non-customer build are refused.
	other := writeCustomer(t, server.URL+"/v1", 5, 1, "line-image-1", &testCustomer{id: "IW-ZZZZ-ZZZZ-ZZZZ", code: c.code, priv: c.priv})
	if _, e = s.ImportCustomerFile(filepath.Join(other.dir, license.FileName)); e == nil || !strings.Contains(e.Error(), "另一份软件") {
		t.Fatal("other software accepted", e)
	}
	bad := filepath.Join(t.TempDir(), "customer.dat")
	os.WriteFile(bad, []byte(strings.Replace(string(want), `"payload": "`, `"payload": "e`, 1)), 0o600)
	if _, e = s.ImportCustomerFile(bad); e == nil {
		t.Fatal("tampered file accepted")
	}
	key := licensePublicKey
	licensePublicKey = "" // a developer build has no key built in
	dev := testService(t)
	dev.LoadLicense(t.TempDir())
	_, e = dev.ImportCustomerFile(filepath.Join(renewed.dir, license.FileName))
	licensePublicKey = key
	if e == nil {
		t.Fatal("developer build imported a customer file")
	}
}

// A customer build (public key built in) without customer.dat is invalid, not a
// free developer build; builds without a key are unchanged; importing the file
// brings the licence back; no update is offered meanwhile.
func TestCustomerBuildWithoutCustomerFile(t *testing.T) {
	isolateLicenseState(t)
	_, server := newKeyRelay(t)

	// Developer builds (no key built in, e.g. build.ps1 without -LicensePublicKey).
	old := licensePublicKey
	licensePublicKey = ""
	dev := testService(t)
	dev.LoadLicense(t.TempDir())
	licensePublicKey = old
	if st := dev.LicenseStatus(); st.Mode != "developer" {
		t.Fatalf("developer build changed: %+v", st)
	}
	if _, e := dev.SaveConnection(ConnectionInput{Name: "开发线路", BaseURL: server.URL + "/v1", Model: "test-image", Key: "k"}); e != nil {
		t.Fatal("developer build cannot add a line", e)
	}

	// Customer build: sign a package (this trusts its key), then lose the file.
	c := writeCustomer(t, server.URL+"/v1", 3, 1, "line-image-1", nil)
	s := testService(t)
	empty := t.TempDir()
	if e := s.LoadLicense(empty); e != nil {
		t.Fatal(e)
	}
	st := s.LicenseStatus()
	if st.Mode != "invalid" || !strings.Contains(st.Message, "customer.dat") {
		t.Fatalf("missing customer.dat: %+v", st)
	}
	if _, e := s.SaveConnection(ConnectionInput{Name: "自己的线路", BaseURL: server.URL + "/v1", Model: "test-image", Key: "own-key"}); e == nil {
		t.Fatal("own line added without customer.dat")
	}
	in := GenerateInput{RequestID: NewID(), ConnectionID: "none", Model: "test-image", Prompts: []string{"p"}, Count: 1, Size: "auto", Quality: "auto", References: []string{}}
	if _, e := s.Submit(in); e == nil {
		t.Fatal("generated without customer.dat")
	}
	// No update while invalid (even with an update address).
	oldURL := updateManifestURL
	updateManifestURL = server.URL + "/update.json"
	defer func() { updateManifestURL = oldURL }()
	if _, e := s.CheckForUpdate(); e == nil || !strings.Contains(e.Error(), "授权无效") {
		t.Fatal("update offered while invalid", e)
	}
	// The customer puts the file back through 导入授权文件.
	st, e := s.ImportCustomerFile(filepath.Join(c.dir, license.FileName))
	if e != nil || st.Mode != "trial" {
		t.Fatalf("import did not restore: %+v %v", st, e)
	}
	if _, e = os.Stat(filepath.Join(empty, license.FileName)); e != nil {
		t.Fatal("customer.dat not written to the program folder")
	}
}
