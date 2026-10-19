package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// These tests run the real product-browser helper and WebView2 against local
// mock pages. scripts/test-product-browser.ps1 builds the helper and sets:
//   IMAGEWORKBENCH_TEST_BROWSER  path to product-browser.exe
//   IMAGEWORKBENCH_TEST_WEBVIEW2 fixed WebView2 runtime directory
// No real site is contacted. The verification case shows the window briefly.

const mockChallenge = `<html><body><p>Please enable JS and disable any ad blocker</p><script>
setTimeout(function(){ document.cookie = 'qa_passed=1; path=/'; location.replace(location.pathname + '?dd_referrer=qa-secret'); }, %d);
</script></body></html>`

func mockProductHTML(server, title string) string {
	return fmt.Sprintf(`<html><head><title>loading</title></head><body><p id="s">加载中</p><script>
setTimeout(function(){
  var s = document.createElement('script'); s.type = 'application/ld+json';
  s.textContent = JSON.stringify({"@type":"Product","name":%q,"description":"由页面脚本渲染的完整商品描述，包含材质、尺寸与工艺。","image":[{"@type":"ImageObject","contentURL":"%s/photo.png"}]});
  document.head.appendChild(s); document.getElementById('s').textContent = '商品已加载';
}, 1500);
</script></body></html>`, title, server)
}

func helperService(t *testing.T) *Service {
	t.Helper()
	exe, runtimeDir := os.Getenv("IMAGEWORKBENCH_TEST_BROWSER"), os.Getenv("IMAGEWORKBENCH_TEST_WEBVIEW2")
	if exe == "" || runtimeDir == "" {
		t.Skip("set IMAGEWORKBENCH_TEST_BROWSER and IMAGEWORKBENCH_TEST_WEBVIEW2 (scripts/test-product-browser.ps1)")
	}
	s := testService(t)
	s.allowLocalProduct = true
	s.productBrowser = productBrowserConfig{Executable: exe, Runtime: runtimeDir}
	return s
}

func mockShop(t *testing.T) *httptest.Server {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		passed, _ := r.Cookie("qa_passed")
		switch r.URL.Path {
		case "/js":
			io.WriteString(w, mockProductHTML(server.URL, "脚本渲染商品"))
		case "/challenge-auto", "/challenge-verify":
			if passed != nil {
				io.WriteString(w, mockProductHTML(server.URL, "验证后商品"))
				return
			}
			delay := 2000
			if r.URL.Path == "/challenge-verify" {
				delay = 7000
			}
			io.WriteString(w, fmt.Sprintf(mockChallenge, delay))
		case "/redirect":
			http.Redirect(w, r, "/js", http.StatusFound)
		case "/blank":
			io.WriteString(w, `<html><body></body></html>`)
		case "/photo.png":
			// Only a real browser gets the image, so the session fallback is exercised.
			if strings.Contains(r.UserAgent(), "ImageWorkbench") {
				w.WriteHeader(403)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write(tinyPNG())
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRealHelperReadsMockPages(t *testing.T) {
	s := helperService(t)
	server := mockShop(t)
	old := productBrowserChallengeAfter
	productBrowserChallengeAfter = 15 * time.Second
	t.Cleanup(func() { productBrowserChallengeAfter = old })
	for path, title := range map[string]string{"/js": "脚本渲染商品", "/redirect": "脚本渲染商品", "/challenge-auto": "验证后商品"} {
		r := productRun(t, s, server.URL+path)
		info, e := s.fetchProduct(context.Background(), r.ID)
		if e != nil || info.Title != title || info.Error != "" || len(info.ReferenceIDs) != 1 {
			t.Fatalf("%s: %+v %v\n%s", path, info, e, logText(t, s))
		}
		saved, _ := s.GetAgentRun(r.ID)
		for _, ev := range saved.Events {
			if ev.Text == productBrowserVerifyStage {
				t.Fatal(path, "verification window shown for a page that passes on its own")
			}
		}
	}
	if strings.Contains(logText(t, s), "qa-secret") {
		t.Fatal("query parameter logged")
	}
}

func TestRealHelperShowsVerificationWindow(t *testing.T) {
	s := helperService(t)
	server := mockShop(t)
	old := productBrowserChallengeAfter
	productBrowserChallengeAfter = 2 * time.Second
	t.Cleanup(func() { productBrowserChallengeAfter = old })
	r := productRun(t, s, server.URL+"/challenge-verify")
	info, e := s.fetchProduct(context.Background(), r.ID)
	if e != nil || info.Title != "验证后商品" {
		t.Fatalf("%+v %v", info, e)
	}
	saved, _ := s.GetAgentRun(r.ID)
	seen := false
	for _, ev := range saved.Events {
		seen = seen || ev.Text == productBrowserVerifyStage
	}
	if !seen {
		t.Fatal("verification stage missing")
	}
}

func TestRealHelperBlankPageTimesOut(t *testing.T) {
	s := helperService(t)
	server := mockShop(t)
	old := productBrowserTimeout
	productBrowserTimeout = 8 * time.Second
	t.Cleanup(func() { productBrowserTimeout = old })
	started := time.Now()
	info, e := s.fetchProduct(context.Background(), productRun(t, s, server.URL+"/blank").ID)
	// The empty direct page keeps its own "补充图片与商品描述" hint; the browser reason is appended.
	if e != nil || !strings.Contains(info.Error, "浏览器读取超时") || !strings.Contains(info.Error, "图片与商品描述") {
		t.Fatalf("%+v %v", info, e)
	}
	if time.Since(started) > 25*time.Second {
		t.Fatal("helper not stopped after timeout")
	}
	if !s.browserMu.TryLock() {
		t.Fatal("profile still locked")
	}
	s.browserMu.Unlock()
}
