package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestImageDownloadRecoversTruncatedResponseWithoutGeneratingAgain(t *testing.T) {
	s := testService(t)
	var generations, downloads atomic.Int32
	image := tinyPNG()
	var source *httptest.Server
	source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			generations.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"data": []Output{{URL: source.URL + "/result?signature=private"}}})
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("download leaked provider authorization")
		}
		if downloads.Add(1) == 1 {
			w.Header().Set("Content-Length", fmt.Sprint(len(image)))
			w.Write(image[:len(image)/2])
			return
		}
		w.Write(image)
	}))
	defer source.Close()
	c := saveConnection(t, s, source.URL)
	if _, e := s.Submit(request(c)); e != nil {
		t.Fatal(e)
	}
	s.stepNext()
	p, e := s.ListTasks("", 0, 10)
	if e != nil || len(p.Items) != 1 || p.Items[0].Status != "succeeded" {
		t.Fatal("download did not recover", p, e)
	}
	if generations.Load() != 1 || downloads.Load() != 2 {
		t.Fatal("wrong request count", generations.Load(), downloads.Load())
	}
	a, e := s.ReadAsset(p.Items[0].AssetIDs[0], false)
	if e != nil || a == "" {
		t.Fatal("recovered image was not saved", e)
	}
}

func TestImageDownloadPermanentErrorsDoNotRetryOrLeakURL(t *testing.T) {
	for _, status := range []int{403, 404, 410, 413} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(status)
			}))
			defer server.Close()
			_, e := downloadImageWithClient(context.Background(), server.URL+"/?signature=private", imageDownloadClient(), 0)
			if e == nil || hits.Load() != 1 || !strings.Contains(e.Error(), fmt.Sprint(status)) || strings.Contains(e.Error(), "private") || strings.Contains(e.Error(), server.URL) {
				t.Fatal(hits.Load(), e)
			}
		})
	}
}

func TestImageDownloadBudgetStopsReadingAndBackoff(t *testing.T) {
	for _, phase := range []string{"body", "backoff"} {
		t.Run(phase, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if phase == "backoff" {
					w.WriteHeader(503)
					return
				}
				w.WriteHeader(200)
				w.Write([]byte("partial"))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			start := time.Now()
			b, e := downloadImageWithClient(ctx, server.URL, imageDownloadClient(), time.Hour)
			if e == nil || b != nil || hits.Load() != 1 || time.Since(start) > 2*time.Second || !strings.Contains(e.Error(), "补下载") {
				t.Fatal("deadline was ignored", hits.Load(), e)
			}
		})
	}
}

func TestImageDownloadTransientHTTPRecovers(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 504} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if hits.Add(1) < 3 {
					w.WriteHeader(status)
					return
				}
				w.Write(tinyPNG())
			}))
			defer server.Close()
			b, e := downloadImageWithClient(context.Background(), server.URL, imageDownloadClient(), 0)
			if e != nil || !bytes.Equal(b, tinyPNG()) || hits.Load() != 3 {
				t.Fatal("GET retry failed", hits.Load(), e)
			}
		})
	}
}

func TestImageDownloadCancellationDoesNotRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := downloadImageWithClient(ctx, "https://example.invalid/?signature=private", imageDownloadClient(), 0)
	if e == nil || !strings.Contains(e.Error(), "已中止") || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
}
