package core

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imageworkbench/internal/update"
)

type updateServer struct {
	manifest, pkg []byte
	priv          ed25519.PrivateKey
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range files {
		f, _ := w.Create(name)
		f.Write([]byte(body))
	}
	w.Close()
	return b.Bytes()
}

// prepareUpdate gives s a program folder, trusts a fresh signing key and serves
// a signed manifest and package for version.
func prepareUpdate(t *testing.T, s *Service, version, minVersion string, corrupt bool) (program string, srv *updateServer) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	t.Cleanup(TrustLicenseKeyForTest(base64.StdEncoding.EncodeToString(pub)))
	program = t.TempDir()
	for name, body := range map[string]string{"ImageWorkbench.exe": "old program", "product-browser.exe": "old browser", "customer.dat": "licence", "data-location.txt": "D:/data", "runtime/msedgewebview2.exe": "runtime-1"} {
		os.MkdirAll(filepath.Dir(filepath.Join(program, name)), 0o700)
		os.WriteFile(filepath.Join(program, name), []byte(body), 0o600)
	}
	rt := sha256.Sum256([]byte("runtime-1"))
	srv = &updateServer{pkg: zipBytes(t, map[string]string{
		"ImageWorkbench.exe":  "new program",
		"product-browser.exe": "new browser",
		"使用说明.txt":            "说明",
		"version.json":        "\xef\xbb\xbf" + `{"version":"` + version + `","runtimeSHA256":"` + strings.ToUpper(hex.EncodeToString(rt[:])) + `"}`,
	})}
	sum := sha256.Sum256(srv.pkg)
	if corrupt {
		sum[0] ^= 1
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "update.json") {
			w.Write(srv.manifest)
			return
		}
		w.Write(srv.pkg)
	}))
	t.Cleanup(server.Close)
	srv.priv = priv
	var e error
	srv.manifest, e = update.Sign(update.Manifest{Version: version, MinVersion: minVersion, Notes: "测试更新", URL: server.URL + "/u.zip", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(srv.pkg))}, priv)
	if e != nil {
		t.Fatal(e)
	}
	old := updateManifestURL
	updateManifestURL = server.URL + "/update.json"
	t.Cleanup(func() { updateManifestURL = old })
	s.mu.Lock()
	s.programDir = program
	s.mu.Unlock()
	return program, srv
}

func TestUpdateFlowReplacesProgramAndKeepsData(t *testing.T) {
	s := testService(t)
	program, _ := prepareUpdate(t, s, "9.9.9", "", false)
	st, e := s.CheckForUpdate()
	if e != nil || !st.Available || st.Version != "9.9.9" || st.Notes != "测试更新" {
		t.Fatalf("check %+v %v", st, e)
	}
	if st, e = s.DownloadUpdate(); e != nil || st.Stage != "ready" {
		t.Fatalf("download %+v %v", st, e)
	}
	// Nothing is replaced until the user installs.
	if read := func(n string) string { b, _ := os.ReadFile(filepath.Join(program, n)); return string(b) }; read("ImageWorkbench.exe") != "old program" {
		t.Fatal("replaced before install")
	}
	// Installing is refused while an image is being generated.
	busy := Task{ID: NewID(), Status: "running", CreatedAt: Now(), UpdatedAt: Now()}
	s.store.Put("task", busy.ID, busy)
	if e = s.StartUpdate(); e == nil || !strings.Contains(e.Error(), "生成") {
		t.Fatal("update started during generation", e)
	}
	// The new program applies itself (as the helper process would).
	s.mu.Lock()
	stage := s.upd.staged
	s.mu.Unlock()
	if e = RunUpdateCommand([]string{"x", updateFlag, "-pid", "0", "-program", program, "-stage", stage}, WaitForExit); e != nil {
		t.Fatal(e)
	}
	read := func(n string) string { b, _ := os.ReadFile(filepath.Join(program, n)); return string(b) }
	if read("ImageWorkbench.exe") != "new program" || read("product-browser.exe") != "new browser" || read("使用说明.txt") != "说明" {
		t.Fatal("program not updated")
	}
	if read("customer.dat") != "licence" || read("data-location.txt") != "D:/data" || read("runtime/msedgewebview2.exe") != "runtime-1" {
		t.Fatal("kept files changed")
	}
	if !strings.Contains(read(".update/update.log"), "更新完成") {
		t.Fatal("no update log")
	}
	CleanupUpdates(program)
	entries, _ := os.ReadDir(filepath.Join(program, updateDir))
	for _, d := range entries {
		if !strings.HasPrefix(d.Name(), "backup-") && d.Name() != "update.log" {
			t.Fatal("left after cleanup:", d.Name())
		}
	}
}

func TestUpdateRefusesBadPackagesAndOldVersions(t *testing.T) {
	s := testService(t)
	program, _ := prepareUpdate(t, s, "9.9.9", "", true)
	s.CheckForUpdate()
	if _, e := s.DownloadUpdate(); e == nil || !strings.Contains(e.Error(), "SHA-256") {
		t.Fatal("corrupt package accepted", e)
	}
	if _, e := os.Stat(filepath.Join(program, updateDir, "9.9.9")); e == nil {
		t.Fatal("corrupt package staged")
	}

	s2 := testService(t)
	prepareUpdate(t, s2, "9.9.9", "9.0.0", false)
	if st, _ := s2.CheckForUpdate(); st.Available || !strings.Contains(st.Error, "完整包") {
		t.Fatalf("update below min version offered: %+v", st)
	}

	s3 := testService(t)
	prepareUpdate(t, s3, AppVersion, "", false)
	if st, _ := s3.CheckForUpdate(); st.Available {
		t.Fatal("same version offered")
	}

	// A build without an update address never checks.
	s4 := testService(t)
	if st := s4.UpdateStatus(); st.Supported {
		t.Fatal("unconfigured build claims updates")
	}
	if _, e := s4.CheckForUpdate(); e == nil {
		t.Fatal("unconfigured build checked")
	}
}

// GitHub Releases answers /releases/latest/download/update.json and the package
// link with a redirect to another host; both are followed and still checked.
func TestUpdateFollowsReleaseRedirects(t *testing.T) {
	s := testService(t)
	program, srv := prepareUpdate(t, s, "9.9.9", "", false)
	assets := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "update.json") {
			w.Write(srv.manifest)
			return
		}
		w.Write(srv.pkg)
	}))
	t.Cleanup(assets.Close)
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, assets.URL+"/blob"+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(releases.Close)
	updateManifestURL = releases.URL + "/owner/repo/releases/latest/download/update.json"
	// The signed manifest names the package on the releases host as well.
	m, _ := update.Verify(srv.manifest, []string{licensePublicKey})
	m.URL = releases.URL + "/owner/repo/releases/download/v9.9.9/ImageWorkbench-update-9.9.9.zip"
	var e error
	if srv.manifest, e = update.Sign(m, srv.priv); e != nil {
		t.Fatal(e)
	}
	st, e := s.CheckForUpdate()
	if e != nil || !st.Available {
		t.Fatalf("redirected manifest: %+v %v", st, e)
	}
	if st, e = s.DownloadUpdate(); e != nil || st.Stage != "ready" {
		t.Fatalf("redirected package: %+v %v", st, e)
	}
	if _, e = os.Stat(filepath.Join(program, updateDir, "9.9.9", "ImageWorkbench.exe")); e != nil {
		t.Fatal("not staged", e)
	}
}
