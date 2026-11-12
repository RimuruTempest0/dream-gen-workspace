package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSignedBuildRefusesLicenceWhenItsSignatureIsBroken(t *testing.T) {
	isolateLicenseState(t)
	_, server := newKeyRelay(t)
	c := writeCustomer(t, server.URL+"/v1", 3, 1, "line-image-1", nil)
	// Unsigned builds (expectSigned empty) are not checked.
	s := testService(t)
	s.LoadLicense(c.dir)
	if st := s.LicenseStatus(); st.Mode != "trial" {
		t.Fatalf("unsigned build: %+v", st)
	}
	// A signed build whose program file carries no valid signature (the test
	// binary has none) refuses the licence.
	old := expectSigned
	expectSigned = "1"
	defer func() { expectSigned = old }()
	s2 := testService(t)
	s2.LoadLicense(c.dir)
	if st := s2.LicenseStatus(); st.Mode != "invalid" || !strings.Contains(st.Message, "数字签名") {
		t.Fatalf("broken signature accepted: %+v", st)
	}
}

func TestAuthenticodeCheck(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	unsigned := filepath.Join(t.TempDir(), "x.exe")
	os.WriteFile(unsigned, []byte("MZ not signed"), 0o600)
	if authenticodeValid(unsigned) {
		t.Fatal("unsigned file accepted")
	}
	// The fixed WebView2 runtime is signed by Microsoft.
	matches, _ := filepath.Glob(filepath.Join("..", "..", ".tools", "webview-expanded", "*", "msedgewebview2.exe"))
	if len(matches) == 0 {
		t.Skip("no signed sample (.tools/webview-expanded) in this checkout")
	}
	if !authenticodeValid(matches[0]) {
		t.Fatal("Microsoft-signed runtime rejected")
	}
}
