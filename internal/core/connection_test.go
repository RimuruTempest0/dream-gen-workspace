package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestModelJSONWithoutStreamEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("model test generated a paid request")
		}
		io.WriteString(w, `{"data":[{"id":"mock-image"}]}`)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	ids, e := NewProvider().Models(ctx, Connection{BaseURL: server.URL}, "mock-key")
	if e != nil || len(ids) != 1 || ids[0] != "mock-image" {
		t.Fatal("model test waited for EOF", e)
	}
}
func TestSavedKeyModelQueryAndCancellation(t *testing.T) {
	s := testService(t)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("saved key not used")
		}
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()
	c := saveConnection(t, s, server.URL)
	done := make(chan error, 1)
	go func() {
		_, e := s.TestConnection(ConnectionInput{ID: c.ID, BaseURL: c.BaseURL, TestRequestID: "cancel-test"})
		done <- e
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("query did not start")
	}
	s.CancelConnectionTest("cancel-test")
	select {
	case e := <-done:
		if e == nil || !strings.Contains(e.Error(), "取消") {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not end request")
	}
}
func TestModelQueryAuthErrorIsVisibleAndRedacted(t *testing.T) {
	s := testService(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"message":"test-key rejected"}}`)
	}))
	defer server.Close()
	result, e := s.TestConnection(ConnectionInput{BaseURL: server.URL, Key: "test-key"})
	if e != nil || !strings.Contains(result.Models, "HTTP 401") || !strings.Contains(result.Auth, "认证被拒绝") || strings.Contains(result.Models, "test-key") {
		t.Fatal(result, e)
	}
}
