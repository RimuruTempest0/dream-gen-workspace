package core

import (
	"context"
	"fmt"
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

func TestInterruptedImageResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		recovered  bool
	}{
		{"complete JSON despite short content length", `{"data":[{"url":"https://example.com/result.png"}]}`, 200, true},
		{"truncated JSON", `{"data":[{"url":"https://example.com/result`, 200, false},
		{"empty data", `{"data":[]}`, 200, false},
		{"invalid image entry", `{"data":[{}]}`, 200, false},
		{"error response cannot recover", `{"data":[{"url":"https://example.com/result.png"}]}`, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.URL.Path != "/images/generations" || r.Header.Get("X-Client-Request-Id") != "local-task" {
					t.Error("wrong request")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-Id", "upstream-test-key")
				w.Header().Set("Content-Length", fmt.Sprint(len(tc.body)+10))
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			var diag GenerationDiagnostic
			outputs, unknown, err := NewProvider().GenerateWithProgress(context.Background(), Task{ID: "local-task", Model: "mock-image", Connection: Connection{BaseURL: server.URL}}, "test-key", nil, "", func(d GenerationDiagnostic) { diag = d })
			if tc.recovered {
				if err != nil || unknown || len(outputs) != 1 {
					t.Fatalf("complete result lost: %v", err)
				}
			} else {
				if err == nil || !unknown || len(outputs) != 0 {
					t.Fatal("partial/error response was treated as success")
				}
			}
			if hits.Load() != 1 {
				t.Fatal("unexpected generation retry")
			}
			if diag.HTTPStatus != tc.status || diag.ContentType != "application/json" || diag.BytesReceived != len(tc.body) || diag.Stage != "receiving" || diag.StartedAt == "" {
				t.Fatalf("missing diagnostics: %+v", diag)
			}
			if strings.Contains(tc.name, "truncated") && diag.NetworkError != "unexpected EOF" {
				t.Fatal("missing truncation error")
			}
			if strings.Contains(diag.UpstreamRequestID, "test-key") || diag.RequestURL != server.URL+"/images/generations" {
				t.Fatal("diagnostics leaked key or wrong URL")
			}
		})
	}
}

func TestCompleteImageJSONDoesNotWaitForStreamEnd(t *testing.T) {
	for _, mode := range []string{"chunked", "wrong content length", "http2"} {
		t.Run(mode, func(t *testing.T) {
			var hits atomic.Int32
			body := `{"data":[{"url":"https://example.com/result.png"}]}`
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				if mode == "wrong content length" {
					w.Header().Set("Content-Length", fmt.Sprint(len(body)+100))
				}
				io.WriteString(w, body)
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Second):
				}
			}))
			if mode == "http2" {
				server.EnableHTTP2 = true
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			provider := &Provider{Client: server.Client()}
			provider.Client.Timeout = 600 * time.Millisecond
			start := time.Now()
			outputs, unknown, err := provider.Generate(context.Background(), Task{Connection: Connection{BaseURL: server.URL}}, "mock-key", nil, "")
			if err != nil || unknown || len(outputs) != 1 || hits.Load() != 1 || time.Since(start) > 500*time.Millisecond {
				t.Fatalf("waited for stream end instead of complete JSON: %v", err)
			}
		})
	}
}

func TestGenerationTimeoutDiagnostics(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(400 * time.Millisecond):
		}
	}))
	defer server.Close()
	provider := NewProvider()
	provider.Client.Timeout = 150 * time.Millisecond
	stages := map[string]bool{}
	var diag GenerationDiagnostic
	_, unknown, err := provider.GenerateWithProgress(context.Background(), Task{Connection: Connection{BaseURL: server.URL}}, "test-key", nil, "", func(d GenerationDiagnostic) { stages[d.Stage] = true; diag = d })
	if err == nil || !unknown || !strings.Contains(err.Error(), "超时") || !stages["waiting"] || diag.NetworkError == "" || diag.HTTPStatus != 0 || hits.Load() != 1 {
		t.Fatalf("wrong timeout behavior: %+v %v", diag, err)
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Fatal("key leaked in network error")
	}
}

func TestEditsDeclareReferenceImageTypes(t *testing.T) {
	root := t.TempDir()
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, make([]byte, 32)...)
	os.MkdirAll(filepath.Join(root, "references"), 0700)
	os.WriteFile(filepath.Join(root, "references", "a.png"), tinyPNG(), 0600)
	os.WriteFile(filepath.Join(root, "references", "b.jpg"), jpeg, 0600)
	refs := []Asset{{Path: "references/a.png"}, {Path: "references/b.jpg"}}
	var types []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" || r.ParseMultipartForm(1<<20) != nil {
			t.Error("not a multipart edit request")
		}
		for _, f := range r.MultipartForm.File["image[]"] {
			types = append(types, f.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"url":"https://example.com/result.png"}]}`)
	}))
	defer server.Close()
	_, _, e := NewProvider().Generate(context.Background(), Task{ID: "edit", Model: "m", Prompt: "p", Connection: Connection{BaseURL: server.URL}}, "k", refs, root)
	if e != nil || strings.Join(types, ",") != "image/png,image/jpeg" {
		t.Fatal("reference parts must carry image MIME types", types, e)
	}
}
