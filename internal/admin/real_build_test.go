//go:build realbuild

package admin

// A manual integration test: builds a complete customer package with the real
// scripts/build.ps1 (several minutes), using fake keys and an isolated admin data
// folder; no network call is made. Run from the repository root:
//
//	go test -tags realbuild -run TestRealCustomerPackage -timeout 20m ./internal/admin
//
// It removes the package it built afterwards.

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imageworkbench/internal/core"
	"imageworkbench/internal/license"
)

func TestRealCustomerPackage(t *testing.T) {
	isolate(t)
	repo, e := filepath.Abs(filepath.Join("..", ".."))
	if e != nil {
		t.Fatal(e)
	}
	a := open(t)
	stock(t, a, 1, "https://relay.example.test/v1")
	c, _ := a.CreateCustomer("集成测试客户", 0)
	if missing, e := a.AutoAssign(c.ID); e != nil || len(missing) != 0 {
		t.Fatal(missing, e)
	}
	if e = a.SetRepoRoot(repo); e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(repo, "release", "customers", c.SoftwareID)
	t.Cleanup(func() { os.RemoveAll(out) })
	d, e := a.BuildPackage(context.Background(), c.ID, "0.6.2", ScriptBuilder{}, LocalDataDir(repo), func(s string) { t.Log(s) })
	if e != nil {
		t.Fatal(e)
	}
	z, e := zip.OpenReader(d.Zip)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	var profile []byte
	names := map[string]bool{}
	for _, f := range z.File {
		names[f.Name] = true
		if f.Name == license.FileName {
			r, _ := f.Open()
			profile, _ = io.ReadAll(r)
			r.Close()
		}
	}
	for _, want := range []string{"ImageWorkbench.exe", "product-browser.exe", "runtime/msedgewebview2.exe", "agent/claude-code/claude.exe", "使用说明.txt", "version.json", "customer.dat"} {
		if !names[want] {
			t.Fatal("package lacks", want)
		}
	}
	for name := range names {
		if strings.Contains(name, "data-location") || strings.HasPrefix(name, "data/") {
			t.Fatal("package carries local data:", name)
		}
	}
	// The program in the package trusts this admin key (built with -LicensePublicKey).
	exe, _ := os.ReadFile(filepath.Join(out, "0.6.2", "ImageWorkbench.exe"))
	if !strings.Contains(string(exe), a.State().SigningPublicKey) {
		t.Fatal("program does not embed the admin public key")
	}
	// The packaged customer.dat activates with the customer's code in the app core.
	defer core.TrustLicenseKeyForTest(a.State().SigningPublicKey)()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, license.FileName), profile, 0o600)
	s, e := core.NewService(filepath.Join(t.TempDir(), "data"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Dispose()
	s.LoadLicense(dir)
	code, _ := a.RevealCode(c.ID)
	if st, e := s.Activate(code); e != nil || st.Mode != "active" || st.SoftwareID != c.SoftwareID {
		t.Fatal(st, e)
	}
	t.Logf("package %s, %d entries, SHA-256 %s", filepath.Base(d.Zip), len(names), d.SHA256)
}
