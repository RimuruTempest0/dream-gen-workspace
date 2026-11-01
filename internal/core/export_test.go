package core

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTenImageSetExportIncludesWholeBatch(t *testing.T) {
	s := testService(t)
	png := tinyPNG()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []Output{{Base64: base64.StdEncoding.EncodeToString(png)}}})
	}))
	defer upstream.Close()
	c := saveConnection(t, s, upstream.URL)
	in := request(c)
	in.Count = 10
	batch, e := s.Submit(in)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 10; i++ {
		if !s.stepNext() {
			t.Fatal("queue ended early")
		}
	}
	other := request(c)
	s.Submit(other)
	s.stepNext()
	recent, e := s.ListRecentBatches(6)
	if e != nil {
		t.Fatal(e)
	}
	var summary RecentBatch
	for _, b := range recent {
		if b.ID == batch {
			summary = b
		}
	}
	if summary.Total != 10 || summary.Completed != 10 || summary.ImageCount != 10 || len(summary.Tasks) != 6 {
		t.Fatalf("incorrect batch summary: %+v", summary)
	}
	encoded, _ := json.Marshal(recent)
	if strings.Contains(string(encoded), "test-key") || strings.Contains(string(encoded), "credential") || strings.Contains(string(encoded), "b64_json") {
		t.Fatal("sensitive snapshot leaked")
	}
	dest := filepath.Join(t.TempDir(), "中文 套图.zip")
	count, e := s.ExportBatch(batch, dest)
	if e != nil || count != 10 {
		t.Fatal(count, e)
	}
	z, e := zip.OpenReader(dest)
	if e != nil {
		t.Fatal(e)
	}
	if len(z.File) != 10 {
		t.Fatal("only preview images exported")
	}
	for i, entry := range z.File {
		if entry.Name != fmt.Sprintf("%03d.png", i+1) {
			t.Fatal(entry.Name)
		}
		reader, _ := entry.Open()
		b, e := io.ReadAll(reader)
		reader.Close()
		if e != nil || !bytes.Equal(b, png) {
			t.Fatal("exported thumbnail or corrupted original")
		}
	}
	z.Close()
	var a Asset
	s.store.Get("asset", summary.Tasks[0].AssetIDs[0], &a)
	source, _ := owned(s.Root(), a.Path)
	os.Remove(source)
	preserved, _ := os.ReadFile(dest)
	if _, e = s.ExportBatch(batch, dest); e == nil {
		t.Fatal("missing original silently skipped")
	}
	after, _ := os.ReadFile(dest)
	if !bytes.Equal(after, preserved) {
		t.Fatal("failed export damaged existing ZIP")
	}
	if _, e = s.ExportBatch("unknown-batch", filepath.Join(t.TempDir(), "empty.zip")); e == nil {
		t.Fatal("empty batch reported success")
	}
}

func TestAssetBatchManagement(t *testing.T) {
	s := testService(t)
	png := tinyPNG()
	ids := []string{}
	for _, name := range []string{"红色卫衣.png", "蓝色.png", "绿色.png"} {
		a, e := s.ImportImage(name, base64.StdEncoding.EncodeToString(png))
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, a.ID)
	}
	p, e := s.SaveProject(Project{Name: "批量项目"})
	if e != nil {
		t.Fatal(e)
	}
	// Duplicates count once; the third asset is untouched.
	if n, e := s.UpdateAssets([]string{ids[0], ids[1], ids[0]}, p.ID, false); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	page, _ := s.ListAssets(p.ID, 0, 24)
	if page.Total != 2 {
		t.Fatal("assets not assigned", page.Total)
	}
	// An unknown id or project rejects the whole batch without partial changes.
	if _, e := s.UpdateAssets([]string{ids[2], "missing"}, p.ID, false); e == nil {
		t.Fatal("unknown asset accepted")
	}
	if _, e := s.UpdateAssets([]string{ids[2]}, "missing-project", false); e == nil {
		t.Fatal("unknown project accepted")
	}
	if page, _ = s.ListAssets(p.ID, 0, 24); page.Total != 2 {
		t.Fatal("rejected batch changed assets", page.Total)
	}
	if _, e := s.UpdateAssets(nil, "", false); e == nil {
		t.Fatal("empty selection accepted")
	}

	dest := filepath.Join(t.TempDir(), "素材.zip")
	if n, e := s.ExportAssets([]string{ids[2], ids[0]}, dest); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	z, e := zip.OpenReader(dest)
	if e != nil {
		t.Fatal(e)
	}
	names := []string{}
	for _, f := range z.File {
		names = append(names, f.Name)
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		if !bytes.Equal(b, png) {
			t.Fatal("exported file is not the original")
		}
	}
	z.Close()
	if strings.Join(names, ",") != "001-绿色.png,002-红色卫衣.png" {
		t.Fatal("unexpected ZIP names", names)
	}

	// Removing hides the assets but keeps their project and files.
	if n, e := s.UpdateAssets([]string{ids[0], ids[2]}, "", true); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	all, _ := s.ListAssets("", 0, 24)
	if all.Total != 1 || all.Items[0].ID != ids[1] {
		t.Fatal("remove did not hide selection", all.Total)
	}
	var hidden Asset
	s.store.Get("asset", ids[0], &hidden)
	if hidden.ProjectID != p.ID {
		t.Fatal("remove cleared the project")
	}
	if source, _ := owned(s.Root(), hidden.Path); fileMissing(source) {
		t.Fatal("remove deleted the file")
	}
}

func fileMissing(path string) bool {
	_, e := os.Stat(path)
	return e != nil
}
