package admin

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// fakeHost stands in for the creative pre-release on GitHub.
type fakeHost struct {
	pre, latest bool
	files       map[string][]byte
	uploads     []string
	// corrupt changes what is served back after an upload.
	corrupt bool
	failOn  string
}

func newFakeHost() *fakeHost { return &fakeHost{pre: true, files: map[string][]byte{}} }

func init() { publishRetry = 0 }

func (h *fakeHost) Release(context.Context) (HostRelease, error) {
	r := HostRelease{Prerelease: h.pre, Latest: h.latest}
	for name := range h.files {
		r.Assets = append(r.Assets, name)
	}
	return r, nil
}

func (h *fakeHost) Upload(_ context.Context, path string) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	name := baseName(path)
	if name == h.failOn {
		return errors.New("network down")
	}
	h.uploads = append(h.uploads, name)
	h.files[name] = b
	return nil
}

func (h *fakeHost) Fetch(_ context.Context, name string) ([]byte, error) {
	b, ok := h.files[name]
	if !ok {
		return nil, ErrNotPublished
	}
	if h.corrupt {
		return append([]byte{}, append(b, 'x')...), nil
	}
	return b, nil
}

func builtCreative(t *testing.T, a *Admin, version string) CreativeRelease {
	t.Helper()
	creativeRepo(t, a, version)
	r, e := a.BuildCreative(context.Background(), version, "说明 "+version, "Notes "+version, fakeBuilder{update: true}, "", func(string) {})
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestUploadCreativeSendsPackageThenManifestAndChecksThePublicCopy(t *testing.T) {
	a := open(t)
	r := builtCreative(t, a, "0.8.0")
	h := newFakeHost()
	plan, e := a.CreativeUploadPlan(context.Background(), "0.8.0", h)
	if e != nil || plan.Published != "" || len(plan.Files) != 2 || plan.Files[0].Name != "ImageWorkbench-update-0.8.0.zip" || plan.Files[1].Name != "update.json" {
		t.Fatalf("plan %+v %v", plan, e)
	}
	got, e := a.UploadCreative(context.Background(), "0.8.0", h, func(string) {})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Join(h.uploads, ",") != "ImageWorkbench-update-0.8.0.zip,update.json" {
		t.Fatal("order", h.uploads)
	}
	if got.UploadedAt == "" || a.State().Creative[0].UploadedAt == "" {
		t.Fatal("upload not recorded")
	}
	want, _ := os.ReadFile(r.Manifest)
	if string(h.files["update.json"]) != string(want) {
		t.Fatal("manifest differs")
	}
	// Uploading the same version again is allowed (a retry); the plan says so.
	if plan, _ = a.CreativeUploadPlan(context.Background(), "0.8.0", h); plan.Published != "0.8.0" {
		t.Fatal("published version not read back", plan)
	}
	if _, e = a.UploadCreative(context.Background(), "0.8.0", h, func(string) {}); e != nil {
		t.Fatal("re-upload refused", e)
	}
}

func TestUploadCreativeRefusesWhatWouldHurtCustomers(t *testing.T) {
	a := open(t)
	builtCreative(t, a, "0.7.39")
	builtCreative(t, a, "0.8.0")
	up := func(h *fakeHost, v string) error {
		_, e := a.UploadCreative(context.Background(), v, h, func(string) {})
		return e
	}
	// The release must stay a pre-release and must never be Latest.
	h := newFakeHost()
	h.pre = false
	if e := up(h, "0.8.0"); e == nil || !strings.Contains(e.Error(), "Pre-release") || len(h.uploads) != 0 {
		t.Fatal("uploaded to a release that is not a pre-release", e)
	}
	h = newFakeHost()
	h.latest = true
	if e := up(h, "0.8.0"); e == nil || !strings.Contains(e.Error(), "Latest") || len(h.uploads) != 0 {
		t.Fatal("uploaded to the Latest release", e)
	}
	// Never go back to an older version.
	h = newFakeHost()
	if e := up(h, "0.8.0"); e != nil {
		t.Fatal(e)
	}
	h.uploads = nil
	if e := up(h, "0.7.39"); e == nil || !strings.Contains(e.Error(), "0.8.0") || len(h.uploads) != 0 {
		t.Fatal("older version replaced a newer one", e)
	}
	// A version that was never built here.
	if e := up(newFakeHost(), "0.9.9"); e == nil {
		t.Fatal("uploaded a version that was not built")
	}
}

func TestUploadCreativeRefusesChangedFilesAndReportsABadPublicCopy(t *testing.T) {
	a := open(t)
	r := builtCreative(t, a, "0.8.0")
	// The package changed after signing: it would fail on every computer.
	orig, _ := os.ReadFile(r.UpdateZip)
	os.WriteFile(r.UpdateZip, append(orig, 'x'), 0o644)
	h := newFakeHost()
	if _, e := a.UploadCreative(context.Background(), "0.8.0", h, func(string) {}); e == nil || len(h.uploads) != 0 {
		t.Fatal("package that does not match update.json uploaded", e)
	}
	os.WriteFile(r.UpdateZip, orig, 0o644)
	// If the package upload fails, update.json is not replaced.
	h = newFakeHost()
	h.failOn = "ImageWorkbench-update-0.8.0.zip"
	if _, e := a.UploadCreative(context.Background(), "0.8.0", h, func(string) {}); e == nil || h.files["update.json"] != nil {
		t.Fatal("manifest replaced although its package did not arrive", e)
	}
	// What GitHub serves back differs: reported, not recorded as uploaded.
	h = newFakeHost()
	h.corrupt = true
	if _, e := a.UploadCreative(context.Background(), "0.8.0", h, func(string) {}); e == nil || !strings.Contains(e.Error(), "不一致") {
		t.Fatal("bad public copy not reported", e)
	}
	if a.State().Creative[0].UploadedAt != "" {
		t.Fatal("recorded as uploaded")
	}
}
