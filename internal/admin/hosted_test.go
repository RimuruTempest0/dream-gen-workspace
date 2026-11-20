package admin

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imageworkbench/internal/license"
)

func zipFile(t *testing.T, path, name string) []byte {
	t.Helper()
	z, e := zip.OpenReader(path)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == name {
			r, _ := f.Open()
			defer r.Close()
			b, _ := io.ReadAll(r)
			return b
		}
	}
	t.Fatal(path, "lacks", name)
	return nil
}

// A hosted user of the mass-market line gets the program built for the line plus
// their own customer.dat; nothing is built for them.
func TestHostedPackageIsTheBuiltMassMarketProgramPlusALicenceFile(t *testing.T) {
	isolate(t)
	a := open(t)
	dir := creativeRepo(t, a, "0.7.33")
	stock(t, a, 2, "http://127.0.0.1:1/v1")
	c, e := a.CreateCustomer("顾客 甲", 5)
	if e != nil || c.Line != CreativeLine {
		t.Fatal(c, e)
	}
	if missing, e := a.AutoAssign(c.ID); e != nil || len(missing) != 0 {
		t.Fatal(missing, e)
	}
	pack := func(version string) (Delivery, error) {
		return a.BuildPackage(context.Background(), c.ID, version, "", func(string) {})
	}

	// Nothing built yet for the line: there is no program to put in the package.
	if s := a.HostedSource(); !strings.Contains(s.Blocked, "发布大众版") {
		t.Fatalf("%+v", s)
	}
	if _, e = pack("0.7.33"); e == nil || !strings.Contains(e.Error(), "发布大众版") {
		t.Fatal("hosted package without a built program:", e)
	}
	// A program from before the licence file was read would ignore it.
	if _, e = a.BuildCreative(context.Background(), "0.7.33", "说明", "Notes", fakeBuilder{update: true}, "", func(string) {}); e != nil {
		t.Fatal(e)
	}
	if _, e = pack("0.7.33"); e == nil || !strings.Contains(e.Error(), HostedSince) {
		t.Fatal("hosted package of a program that ignores the licence file:", e)
	}

	sourceAt(a, "0.7.34")
	if _, e = a.BuildCreative(context.Background(), "0.7.34", "说明", "Notes", fakeBuilder{update: true}, "", func(string) {}); e != nil {
		t.Fatal(e)
	}
	if s := a.HostedSource(); s.Version != "0.7.34" || s.Blocked != "" {
		t.Fatalf("%+v", s)
	}
	// Always the newest built version.
	if _, e = pack("0.7.33"); e == nil || !strings.Contains(e.Error(), "0.7.34") {
		t.Fatal("hosted package of an older version:", e)
	}
	d, e := pack("0.7.34")
	if e != nil {
		t.Fatal(e)
	}
	if d.Zip != filepath.Join(dir, "release", "customers", c.SoftwareID, "ImageWorkbench-顾客 甲-0.7.34.zip") {
		t.Fatalf("%+v", d)
	}
	names := strings.Join(zipNames(t, d.Zip), " ")
	for _, want := range []string{"ImageWorkbench.exe", "product-browser.exe", "runtime/msedgewebview2.exe", "agent/claude-code/claude.exe", "version.json", "customer.dat"} {
		if !strings.Contains(names, want) {
			t.Fatal("hosted package lacks", want, names)
		}
	}
	// The program in it is the one built for the line, byte for byte.
	program := filepath.Join(dir, "release", "creative", "build", "0.7.34")
	exe, _ := os.ReadFile(filepath.Join(program, "ImageWorkbench.exe"))
	if string(zipFile(t, d.Zip, "ImageWorkbench.exe")) != string(exe) {
		t.Fatal("the hosted package carries another program")
	}
	// The licence file is this person's, signed with the key the line's programs
	// trust, and it did not land in the program folder later builds compare with.
	p, e := license.Verify(zipFile(t, d.Zip, "customer.dat"), []string{a.State().SigningPublicKey})
	if e != nil || p.SoftwareID != c.SoftwareID || p.Customer != "顾客 甲" || p.TrialImages != 5 {
		t.Fatal(p, e)
	}
	if fileExists(filepath.Join(program, license.FileName)) {
		t.Fatal("customer.dat was written into the line's program folder")
	}
	got := a.State().Customers[0]
	if len(got.Deliveries) != 1 || got.Changed || got.Line != CreativeLine {
		t.Fatalf("%+v", got)
	}

	// Someone who already has the program gets the licence file alone.
	other, _ := a.CreateCustomer("顾客 乙", 0)
	a.AutoAssign(other.ID)
	dest := filepath.Join(t.TempDir(), "customer.dat")
	if e = a.WriteCustomerFile(other.ID, dest); e != nil {
		t.Fatal("licence file for a hosted user without a package:", e)
	}
	data, _ := os.ReadFile(dest)
	if p, e = license.Verify(data, []string{a.State().SigningPublicKey}); e != nil || p.SoftwareID != other.SoftwareID {
		t.Fatal(p, e)
	}
}

// A record left from the retired e-commerce line (no line marker) gets neither a
// package nor a licence file.
func TestRetiredEcommerceRecordsAreRefused(t *testing.T) {
	isolate(t)
	a := open(t)
	repo(t, a)
	stock(t, a, 1, "http://127.0.0.1:1/v1")
	c, _ := a.CreateCustomer("顾客 丙", 0)
	a.AutoAssign(c.ID)
	if c.Line != CreativeLine {
		t.Fatal("a new person is not a hosted user:", c.Line)
	}
	a.mu.Lock()
	old, _ := a.customerLocked(c.ID)
	old.Line = ""
	a.mu.Unlock()
	if e := a.WriteCustomerFile(c.ID, filepath.Join(t.TempDir(), "customer.dat")); e == nil || !strings.Contains(e.Error(), "电商版") {
		t.Fatal("licence file for a retired e-commerce record:", e)
	}
	if _, e := a.BuildPackage(context.Background(), c.ID, "0.7.34", "", func(string) {}); e == nil || !strings.Contains(e.Error(), "电商版") {
		t.Fatal("package for a retired e-commerce record:", e)
	}
}
