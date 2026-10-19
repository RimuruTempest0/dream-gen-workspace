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
