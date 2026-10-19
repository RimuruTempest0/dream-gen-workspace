package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func tinyPNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.NRGBA{40, 90, 160, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}
func testService(t *testing.T) *Service {
	t.Helper()
	s, e := NewService(filepath.Join(t.TempDir(), "中文 数据"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Dispose)
	return s
}
func saveConnection(t *testing.T, s *Service, base string) Connection {
	t.Helper()
	c, e := s.SaveConnection(ConnectionInput{Name: "测试连接", BaseURL: base, Model: "test-image", Key: "test-key"})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func request(c Connection) GenerateInput {
	return GenerateInput{RequestID: NewID(), ConnectionID: c.ID, Model: c.Model, Prompts: []string{"test prompt"}, Count: 1, Size: "auto", Quality: "auto", References: []string{}}
}
func TestURLAndKeyBoundary(t *testing.T) {
	s := testService(t)
	for _, bad := range []string{"ftp://x", "https://user:pass@x/v1", "https://x/v1?key=x", "https://x/#fragment"} {
		if _, e := NormalizeURL(bad); e == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	c := saveConnection(t, s, "http://localhost:8090/custom/v1/")
	if c.BaseURL != "http://localhost:8090/custom/v1" || !c.HasKey || c.Credential != "" {
		t.Fatal(c)
	}
	cs, _ := s.ListConnections()
	b, _ := json.Marshal(cs)
	if strings.Contains(string(b), "test-key") {
		t.Fatal("key exposed")
	}
	files, _ := os.ReadDir(filepath.Join(s.Root(), "secrets"))
	encrypted, _ := os.ReadFile(filepath.Join(s.Root(), "secrets", files[0].Name()))
	if bytes.Contains(encrypted, []byte("test-key")) {
		t.Fatal("plain key on disk")
	}
	c2, e := s.SaveConnection(ConnectionInput{ID: c.ID, Name: c.Name, BaseURL: c.BaseURL, Model: c.Model})
	if e != nil || !c2.HasKey {
		t.Fatal("blank key did not retain", e)
	}
}
func TestTwoProvidersSnapshotDedupAndEdit(t *testing.T) {
	s := testService(t)
	var hits atomic.Int32
	pngData := base64.StdEncoding.EncodeToString(tinyPNG())
	provider := func(prefix string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, prefix) {
				t.Errorf("wrong prefix %s", r.URL.Path)
				w.WriteHeader(404)
				return
			}
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Error("missing auth")
			}
			if strings.HasSuffix(r.URL.Path, "/models") {
				io.WriteString(w, `{"data":[{"id":"test-image"}]}`)
				return
			}
			hits.Add(1)
			if strings.HasSuffix(r.URL.Path, "/edits") {
				if e := r.ParseMultipartForm(4 << 20); e != nil {
					t.Error(e)
				}
				if len(r.MultipartForm.File["image"]) != 1 {
					t.Error("reference missing")
				}
			} else {
				var m map[string]any
				json.NewDecoder(r.Body).Decode(&m)
				if _, ok := m["quality"]; ok {
					t.Error("auto quality should be omitted")
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"data": []Output{{Base64: pngData}}})
		}))
	}
	a := provider("/prefix/v1")
	defer a.Close()
	b := provider("/another")
	defer b.Close()
	c := saveConnection(t, s, a.URL+"/prefix/v1")
	d := saveConnection(t, s, b.URL+"/another")
	for _, conn := range []Connection{c, d} {
		result, e := s.TestConnection(ConnectionInput{ID: conn.ID, BaseURL: conn.BaseURL})
		if e != nil || len(result.IDs) != 1 {
			t.Fatal(result, e)
		}
	}
	ref, e := s.ImportImage("reference.png", pngData)
	if e != nil {
		t.Fatal(e)
	}
	in := request(c)
	in.References = []string{ref.ID}
	batch, e := s.Submit(in)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Submit(in)
	if e != nil || batch != again {
		t.Fatal("submission not deduplicated")
	}
	if _, e = s.SaveConnection(ConnectionInput{ID: c.ID, Name: c.Name, BaseURL: b.URL + "/another", Model: c.Model}); e != nil {
		t.Fatal(e)
	}
	if !s.stepNext() {
		t.Fatal("worker didn't run")
	}
	page, e := s.ListTasks("", 0, 10)
	if e != nil || len(page.Items) != 1 || page.Items[0].Status != "succeeded" {
		t.Fatal(page, e)
	}
	if page.Items[0].Connection.BaseURL != a.URL+"/prefix/v1" {
		t.Fatal("snapshot changed")
	}
	if hits.Load() != 1 {
		t.Fatal("duplicate generation")
	}
	if _, e = s.Submit(request(d)); e != nil {
		t.Fatal(e)
	}
	s.stepNext()
	if hits.Load() != 2 {
		t.Fatal("second provider unused")
	}
}
func TestUncertainNoRetryAndRecover(t *testing.T) {
	s := testService(t)
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(503)
		io.WriteString(w, `{"error":{"message":"test-key upstream timeout"}}`)
	}))
	defer up.Close()
	c := saveConnection(t, s, up.URL+"/v1")
	s.Submit(request(c))
	s.stepNext()
	s.stepNext()
	p, _ := s.ListTasks("", 0, 10)
	if p.Items[0].Status != "unknown" || hits.Load() != 1 || strings.Contains(p.Items[0].Error, "test-key") {
		t.Fatal(p, hits.Load())
	}
	if p.Items[0].Diagnostic.HTTPStatus != 503 || p.Items[0].Diagnostic.StartedAt == "" || p.Items[0].Diagnostic.BytesReceived == 0 {
		t.Fatal("request diagnostics were not persisted")
	}
	var task Task
	s.store.Get("task", p.Items[0].ID, &task)
	task.Status = "submitting"
	s.store.Put("task", task.ID, task)
	root := s.Root()
	s.store.Close()
	reopened, e := NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Dispose()
	p, _ = reopened.ListTasks("", 0, 10)
	if p.Items[0].Status != "unknown" {
		t.Fatal("submitted task requeued")
	}
	if p.Items[0].Diagnostic.HTTPStatus != 503 {
		t.Fatal("request diagnostics lost on restart")
	}
}
func TestDownloadDoesNotLeakAuthOrRegenerate(t *testing.T) {
	s := testService(t)
	var downloads atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("credential leaked")
		}
		if downloads.Load() <= imageDownloadAttempts {
			w.WriteHeader(503)
			return
		}
		w.Write(tinyPNG())
	}))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer redirect.Close()
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"data": []Output{{URL: redirect.URL}}})
	}))
	defer up.Close()
	c := saveConnection(t, s, up.URL)
	s.Submit(request(c))
	s.stepNext()
	p, _ := s.ListTasks("", 0, 10)
	if p.Items[0].Status != "download_failed" {
		t.Fatal(p)
	}
	if downloads.Load() != imageDownloadAttempts || !strings.Contains(p.Items[0].Error, "HTTP 503") {
		t.Fatal("temporary downloads were not retried", downloads.Load(), p.Items[0].Error)
	}
	if e := s.RetryDownload(p.Items[0].ID); e != nil {
		t.Fatal(e)
	}
	s.stepNext()
	p, _ = s.ListTasks("", 0, 10)
	if p.Items[0].Status != "succeeded" || calls.Load() != 1 {
		t.Fatal("retry generated again", p)
	}
}
func TestBackupRestoreExcludesKeysAndZipTraversal(t *testing.T) {
	s := testService(t)
	c := saveConnection(t, s, "http://localhost:8000/v1")
	ref, e := s.ImportImage("商品.png", base64.StdEncoding.EncodeToString(tinyPNG()))
	if e != nil {
		t.Fatal(e)
	}
	s.Submit(request(c))
	path := filepath.Join(t.TempDir(), "backup.zip")
	if e = s.CreateBackup(path); e != nil {
		t.Fatal(e)
	}
	z, e := zip.OpenReader(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range z.File {
		if strings.Contains(f.Name, "secrets") {
			t.Fatal("secrets included")
		}
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		if bytes.Contains(b, []byte("test-key")) {
			t.Fatal("key in backup")
		}
	}
	z.Close()
	s.StopAllQueued()
	if e = s.RestoreBackup(path); e != nil {
		t.Fatal(e)
	}
	cs, _ := s.ListConnections()
	if cs[0].HasKey {
		t.Fatal("key restored")
	}
	if _, e = s.ReadAsset(ref.ID, false); e != nil {
		t.Fatal(e)
	}
	p, _ := s.ListTasks("", 0, 10)
	if p.Items[0].Status != "blocked" {
		t.Fatal("restored queue not blocked")
	}
	bad := filepath.Join(t.TempDir(), "bad.zip")
	f, _ := os.Create(bad)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("images/../../outside.txt")
	w.Write([]byte("bad"))
	zw.Close()
	f.Close()
	if e = s.RestoreBackup(bad); e == nil {
		t.Fatal("zip slip accepted")
	}
}
func TestCRUDAndShutdown(t *testing.T) {
	s := testService(t)
	p, e := s.SaveProject(Project{Name: "商品项目"})
	if e != nil {
		t.Fatal(e)
	}
	tpl, e := s.SaveTemplate(Template{Name: "白底", Prompt: "white background"})
	if e != nil {
		t.Fatal(e)
	}
	s.DeleteTemplate(tpl.ID)
	templates, _ := s.ListTemplates()
	if len(templates) != 0 {
		t.Fatal("template not deleted")
	}
	s.DeleteProject(p.ID)
	s.Start(nil)
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker does not stop")
	}
}
func TestMalformedProviderAndReferenceValidation(t *testing.T) {
	s := testService(t)
	if _, e := s.ImportImage("bad.png", base64.StdEncoding.EncodeToString([]byte("bad"))); e == nil {
		t.Fatal("bad image accepted")
	}
	if _, e := owned(s.Root(), "../escape"); e == nil {
		t.Fatal("escape accepted")
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":[]}`)) }))
	defer up.Close()
	out, unknown, e := s.provider.Generate(context.Background(), Task{Model: "x", Prompt: "p", Connection: Connection{BaseURL: up.URL}}, "test-key", nil, s.Root())
	if e == nil || !unknown || len(out) > 0 {
		t.Fatal("malformed completion not uncertain")
	}
}
