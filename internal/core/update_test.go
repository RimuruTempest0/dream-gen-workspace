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
	"time"

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
	m, _ := update.Verify(srv.manifest, []string{licensePublicKey}, "")
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

// creativeUpdate turns s into a product-line build ("creative": no licence, its
// own update key) and serves an update signed for line.
func creativeUpdate(t *testing.T, s *Service, signedFor, packageLine string) (program string, srv *updateServer) {
	t.Helper()
	program, srv = prepareUpdate(t, s, "9.9.9", "", false)
	oldLine, oldKey, oldLicence := productLine, updatePublicKey, licensePublicKey
	productLine, updatePublicKey, licensePublicKey = "creative", licensePublicKey, ""
	t.Cleanup(func() { productLine, updatePublicKey, licensePublicKey = oldLine, oldKey, oldLicence })
	m, e := update.Verify(srv.manifest, []string{updatePublicKey}, "")
	if e != nil {
		t.Fatal(e)
	}
	rt := sha256.Sum256([]byte("runtime-1"))
	srv.pkg = zipBytes(t, map[string]string{
		"ImageWorkbench.exe": "new program",
		"version.json":       `{"version":"9.9.9","productLine":"` + packageLine + `","runtimeSHA256":"` + hex.EncodeToString(rt[:]) + `"}`,
	})
	sum := sha256.Sum256(srv.pkg)
	m.Line, m.SHA256, m.Size = signedFor, hex.EncodeToString(sum[:]), int64(len(srv.pkg))
	if srv.manifest, e = update.Sign(m, srv.priv); e != nil {
		t.Fatal(e)
	}
	return program, srv
}

// The mass-market line has no licence file and still updates, from manifests of
// its own line only.
func TestProductLineUpdatesWithoutLicence(t *testing.T) {
	s := testService(t)
	creativeUpdate(t, s, "creative", "creative")
	if st := s.UpdateStatus(); !st.Supported {
		t.Fatal("product-line build without a licence key cannot update")
	}
	if s.LicenseStatus().Mode != "developer" {
		t.Fatal("product-line build asks for a licence:", s.LicenseStatus().Mode)
	}
	st, e := s.CheckForUpdate()
	if e != nil || !st.Available {
		t.Fatalf("own line: %+v %v", st, e)
	}
	if st, e = s.DownloadUpdate(); e != nil || st.Stage != "ready" {
		t.Fatalf("download: %+v %v", st, e)
	}

	// An e-commerce manifest (format 1), same key, is not this line's update.
	s2 := testService(t)
	creativeUpdate(t, s2, "", "creative")
	if st, e := s2.CheckForUpdate(); e == nil || st.Available {
		t.Fatalf("e-commerce manifest offered to the creative line: %+v", st)
	}

	// Nor is a package built for another line, whatever the manifest says.
	s3 := testService(t)
	program, _ := creativeUpdate(t, s3, "creative", "")
	s3.CheckForUpdate()
	if _, e := s3.DownloadUpdate(); e == nil || !strings.Contains(e.Error(), "产品线") {
		t.Fatal("package of another line staged", e)
	}
	if _, e := os.Stat(filepath.Join(program, updateDir, "9.9.9")); e == nil {
		t.Fatal("foreign package left staged")
	}
}

// One click: download, check, restart. While an image is being generated the
// update waits and says so, then restarts by itself once the image is done.
func TestUpdateNowWaitsForRunningImagesThenRestarts(t *testing.T) {
	s := testService(t)
	prepareUpdate(t, s, "9.9.9", "", false)
	started := make(chan []string, 1)
	oldStart, oldPoll := startUpdater, updateIdlePoll
	startUpdater = func(exe string, args ...string) error { started <- append([]string{exe}, args...); return nil }
	updateIdlePoll = 10 * time.Millisecond
	t.Cleanup(func() { startUpdater, updateIdlePoll = oldStart, oldPoll })
	busy := Task{ID: NewID(), Status: "running", CreatedAt: Now(), UpdatedAt: Now()}
	s.store.Put("task", busy.ID, busy)
	if _, e := s.CheckForUpdate(); e != nil {
		t.Fatal(e)
	}
	restarted := make(chan struct{})
	if e := s.UpdateNow(func() { close(restarted) }); e != nil {
		t.Fatal(e)
	}
	// A second click while it is under way changes nothing.
	if e := s.UpdateNow(func() { t.Error("second restart") }); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := s.UpdateStatus()
		if st.Stage == "ready" && st.Installing && strings.Contains(st.Waiting, "图片") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not waiting for the image: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-restarted:
		t.Fatal("restarted while an image was running")
	default:
	}
	busy.Status = "succeeded"
	s.store.Put("task", busy.ID, busy)
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("did not restart after the image finished")
	}
	args := <-started
	if !strings.HasSuffix(args[0], "ImageWorkbench.exe") || args[1] != updateFlag {
		t.Fatal("updater not started", args)
	}
}

// While the update waits for images, a routine check leaves it alone, and the
// user can call the wait off; the checked download is kept for the next click.
func TestWaitingUpdateSurvivesChecksAndCanBeCalledOff(t *testing.T) {
	s := testService(t)
	prepareUpdate(t, s, "9.9.9", "", false)
	oldStart, oldPoll := startUpdater, updateIdlePoll
	startUpdater = func(string, ...string) error { t.Error("restarted"); return nil }
	updateIdlePoll = 10 * time.Millisecond
	t.Cleanup(func() { startUpdater, updateIdlePoll = oldStart, oldPoll })
	busy := Task{ID: NewID(), Status: "running", CreatedAt: Now(), UpdatedAt: Now()}
	s.store.Put("task", busy.ID, busy)
	s.CheckForUpdate()
	if e := s.UpdateNow(func() { t.Error("restart called") }); e != nil {
		t.Fatal(e)
	}
	wait := func(what string, ok func(UpdateStatus) bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ok(s.UpdateStatus()) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: %+v", what, s.UpdateStatus())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait("not waiting", func(st UpdateStatus) bool { return st.Installing && st.Waiting != "" })
	if st, e := s.CheckForUpdate(); e != nil || st.Stage != "ready" || !st.Installing {
		t.Fatalf("a check disturbed the waiting update: %+v %v", st, e)
	}
	s.CancelUpdateWait()
	wait("still waiting after cancel", func(st UpdateStatus) bool { return !st.Installing })
	if st := s.UpdateStatus(); st.Stage != "ready" || st.Waiting != "" || st.Error != "" || !st.Available {
		t.Fatalf("after calling it off: %+v", st)
	}
}

// A failed download ends the one-click update with the reason; it does not
// restart and can be tried again.
func TestUpdateNowStopsOnBadDownload(t *testing.T) {
	s := testService(t)
	prepareUpdate(t, s, "9.9.9", "", true)
	s.CheckForUpdate()
	if e := s.UpdateNow(func() { t.Error("restarted after a bad download") }); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := s.UpdateStatus()
		if !st.Installing && strings.Contains(st.Error, "SHA-256") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no error: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
