package core

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFakeProductBrowser is not a test: the browser tests re-run the test binary
// with IWB_FAKE_BROWSER set so it speaks the helper protocol without WebView2.
func TestFakeProductBrowser(t *testing.T) {
	scenario := os.Getenv("IWB_FAKE_BROWSER")
	if scenario == "" {
		t.Skip("helper process only")
	}
	args := map[string]string{}
	for i, a := range os.Args {
		if a == "--" {
			rest := os.Args[i+1:]
			for j := 0; j+1 < len(rest); j += 2 {
				args[strings.TrimPrefix(rest[j], "-")] = rest[j+1]
			}
		}
	}
	emit := func(v map[string]any) {
		b, _ := json.Marshal(v)
		os.Stdout.Write(append(b, '\n'))
	}
	if _, e := os.Stat(args["profile"]); e != nil {
		emit(map[string]any{"event": "error", "code": "arguments", "message": "profile missing"})
		os.Exit(1)
	}
	emit(map[string]any{"event": "started"})
	page := func(url string) {
		emit(map[string]any{"event": "page", "url": url, "html": os.Getenv("IWB_FAKE_HTML")})
	}
	switch scenario {
	case "page":
		page(args["url"])
	case "verify":
		emit(map[string]any{"event": "challenge"})
		emit(map[string]any{"event": "verify"})
		time.Sleep(2500 * time.Millisecond)
		page(args["url"] + "?dd_referrer=secret-token")
	case "closed":
		emit(map[string]any{"event": "verify"})
		emit(map[string]any{"event": "error", "code": "closed", "message": "验证窗口已关闭"})
		os.Exit(1)
	case "private":
		page("http://127.0.0.1/private")
	case "crash":
		os.Exit(3)
	case "hang":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var op struct{ Op, ID, URL string }
		json.Unmarshal(scanner.Bytes(), &op)
		if op.Op == "quit" {
			break
		}
		if op.Op == "image" {
			emit(map[string]any{"event": "image", "id": op.ID, "status": 200, "mime": "image/png", "data": base64.StdEncoding.EncodeToString(tinyPNG())})
		}
	}
	os.Exit(0)
}

func fakeBrowserService(t *testing.T, scenario, html string) *Service {
	t.Helper()
	t.Setenv("IWB_FAKE_BROWSER", scenario)
	t.Setenv("IWB_FAKE_HTML", html)
	s := testService(t)
	s.allowLocalProduct = true
	s.productBrowser = productBrowserConfig{Executable: os.Args[0], Runtime: "unused", Args: []string{"-test.run=^TestFakeProductBrowser$", "--"}}
	return s
}

func productRun(t *testing.T, s *Service, link string) AgentRun {
	t.Helper()
	r := AgentRun{ID: NewID(), Input: AgentInput{ProductURL: link}, CreatedAt: Now()}
	if e := s.store.Put("agent_run", r.ID, r); e != nil {
		t.Fatal(e)
	}
	return r
}

func logText(t *testing.T, s *Service) string {
	t.Helper()
	page, e := s.ListLogs("", "", "", 0, 200)
	if e != nil {
		t.Fatal(e)
	}
	var b strings.Builder
	for _, item := range page.Items {
		b.WriteString(item.Message + "\n")
	}
	return b.String()
}

func productServer(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image.png":
			w.Write(tinyPNG())
		case "/challenge":
			io.WriteString(w, `<html><body>Please enable JS and disable any ad blocker<iframe src="https://geo.captcha-delivery.com/c"></iframe></body></html>`)
		case "/amazon-block":
			io.WriteString(w, amazonBlockPage)
		case "/amazon-block-text":
			io.WriteString(w, `<html><head><title>Amazon.com</title></head><body><h4>Click the button below
 to continue shopping</h4><button>Continue shopping</button></body></html>`)
		case "/empty":
			io.WriteString(w, `<html><body></body></html>`)
		default:
			w.WriteHeader(403)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func productPage(server string) string {
	return fmt.Sprintf(`<html><head><script type="application/ld+json">{"@type":"Product","name":"浏览器商品","description":"浏览器渲染后才出现的完整商品描述，包含材质与尺寸。","image":[{"@type":"ImageObject","contentURL":"%[1]s/image.png"},{"@type":"ImageObject","contentURL":"%[1]s/blocked-image.png"}],"offers":{"price":"9.45"}}</script></head><body>商品</body></html>`, server)
}

func TestBrowserFallbackReadsBlockedPageAndImages(t *testing.T) {
	server := productServer(t)
	s := fakeBrowserService(t, "page", productPage(server.URL))
	r := productRun(t, s, server.URL+"/listing?ref=secret-query")
	info, e := s.fetchProduct(context.Background(), r.ID)
	if e != nil || info.Title != "浏览器商品" || info.Error != "" || len(info.ReferenceIDs) != 2 {
		t.Fatalf("%+v %v", info, e)
	}
	if info.SourceURL != r.Input.ProductURL {
		t.Fatal("source URL replaced by browser URL", info.SourceURL)
	}
	logs := logText(t, s)
	if !strings.Contains(logs, "HTTP 403") || !strings.Contains(logs, "浏览器读取商品页完成") || strings.Contains(logs, "secret-query") || strings.Contains(logs, "完整商品描述") {
		t.Fatal("unexpected logs", logs)
	}
	if !s.browserMu.TryLock() {
		t.Fatal("browser session not closed")
	}
	s.browserMu.Unlock()
}

func TestBrowserFallbackOnChallengeAndEmptyPages(t *testing.T) {
	server := productServer(t)
	for _, path := range []string{"/challenge", "/empty"} {
		s := fakeBrowserService(t, "page", productPage(server.URL))
		info, e := s.fetchProduct(context.Background(), productRun(t, s, server.URL+path).ID)
		if e != nil || info.Title != "浏览器商品" {
			t.Fatal(path, info, e)
		}
	}
}

func TestBrowserVerificationProgress(t *testing.T) {
	server := productServer(t)
	s := fakeBrowserService(t, "verify", productPage(server.URL))
	r := productRun(t, s, server.URL+"/listing")
	info, e := s.fetchProduct(context.Background(), r.ID)
	if e != nil || info.Title != "浏览器商品" {
		t.Fatal(info, e)
	}
	saved, _ := s.GetAgentRun(r.ID)
	var stages []string
	for _, ev := range saved.Events {
		stages = append(stages, ev.Text)
	}
	joined := strings.Join(stages, "|")
	for _, want := range []string{productBrowserReadingStage, productBrowserVerifyStage} {
		if !strings.Contains(joined, want) {
			t.Fatal("missing stage", want, joined)
		}
	}
	if strings.Contains(logText(t, s), "secret-token") {
		t.Fatal("query leaked to logs")
	}
}

func TestBrowserFailuresKeepUploadHint(t *testing.T) {
	oldTimeout := productBrowserTimeout
	productBrowserTimeout = 2 * time.Second
	t.Cleanup(func() { productBrowserTimeout = oldTimeout })
	server := productServer(t)
	cases := map[string]string{"closed": "验证窗口已关闭", "crash": "意外退出", "hang": "超时"}
	for scenario, want := range cases {
		s := fakeBrowserService(t, scenario, "")
		started := time.Now()
		info, e := s.fetchProduct(context.Background(), productRun(t, s, server.URL+"/listing").ID)
		if e != nil || !strings.Contains(info.Error, want) || !strings.Contains(info.Error, "请上传图片或粘贴商品描述") || info.Title != "" {
			t.Fatal(scenario, info, e)
		}
		if time.Since(started) > 15*time.Second {
			t.Fatal(scenario, "helper was not stopped promptly")
		}
		if !strings.Contains(logText(t, s), "浏览器读取商品页失败") {
			t.Fatal(scenario, "failure not logged")
		}
	}
}

func TestBrowserCancellationStopsHelper(t *testing.T) {
	server := productServer(t)
	s := fakeBrowserService(t, "hang", "")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(time.Second, cancel)
	started := time.Now()
	info, _ := s.fetchProduct(ctx, productRun(t, s, server.URL+"/listing").ID)
	if time.Since(started) > 10*time.Second || info.Error == "" {
		t.Fatal("cancel did not stop helper", info)
	}
	if !s.browserMu.TryLock() {
		t.Fatal("profile still locked")
	}
	s.browserMu.Unlock()
}

func TestBrowserOutputLimitsAndPrivateRedirect(t *testing.T) {
	server := productServer(t)
	old := productBrowserMaxHTML
	productBrowserMaxHTML = 64
	s := fakeBrowserService(t, "page", productPage(server.URL))
	info, e := s.fetchProduct(context.Background(), productRun(t, s, server.URL+"/listing").ID)
	productBrowserMaxHTML = old
	if e != nil || !strings.Contains(info.Error, "大小限制") {
		t.Fatal(info, e)
	}
	s = fakeBrowserService(t, "private", productPage(server.URL))
	s.allowLocalProduct = false
	if _, _, _, e := s.productFromBrowser(context.Background(), NewID(), "https://example.com/item", "HTTP 403"); e == nil || !strings.Contains(e.Error(), "不受支持的地址") {
		t.Fatal("private redirect accepted", e)
	}
}

func TestBrowserNotUsedForInvalidOrMissingTargets(t *testing.T) {
	ctx := context.Background()
	if browserFallbackReason(ctx, nil, ProductInfo{}, productTargetError{"private"}) != "" {
		t.Fatal("private target retried in browser")
	}
	if browserFallbackReason(ctx, nil, ProductInfo{}, productHTTPError{404}) != "" {
		t.Fatal("404 retried in browser")
	}
	if browserFallbackReason(ctx, nil, ProductInfo{}, productHTTPError{429}) == "" {
		t.Fatal("rate limit not retried")
	}
	s := testService(t)
	s.allowLocalProduct = true
	s.productBrowser = productBrowserConfig{Executable: filepath.Join(t.TempDir(), "missing.exe")}
	server := productServer(t)
	info, e := s.fetchProduct(ctx, productRun(t, s, server.URL+"/listing").ID)
	if e != nil || !strings.Contains(info.Error, "缺少浏览器辅助程序") || !strings.Contains(info.Error, "请上传图片或粘贴商品描述") {
		t.Fatal(info, e)
	}
}

func TestClearProductBrowserData(t *testing.T) {
	s := testService(t)
	profile := s.productBrowserProfile()
	os.MkdirAll(filepath.Join(profile, "EBWebView"), 0700)
	os.WriteFile(filepath.Join(profile, "EBWebView", "Cookies"), []byte("x"), 0600)
	if e := s.ClearProductBrowserData(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(profile); !os.IsNotExist(e) {
		t.Fatal("profile kept", e)
	}
	s.browserMu.Lock()
	if s.ClearProductBrowserData() == nil {
		t.Fatal("cleared while a page is being read")
	}
	s.browserMu.Unlock()
}

func TestProductImageObjectContentURL(t *testing.T) {
	_, images, e := parseProductPage([]byte(`<meta property="og:image" content="https://cdn.example/og.jpg"><script type="application/ld+json">{"@type":"Product","name":"x","image":[{"@type":"ImageObject","contentURL":"https://cdn.example/a.jpg","thumbnail":"https://cdn.example/t.jpg"},{"contentUrl":"https://cdn.example/b.jpg"}]}</script><img src="https://cdn.example/recommended.jpg">`), "https://shop.example/item")
	if e != nil || len(images) != 2 || images[0] != "https://cdn.example/a.jpg" || images[1] != "https://cdn.example/b.jpg" {
		t.Fatal(images, e)
	}
}

func TestPublicHostLookupRetriesAndCachesOnlyPublicResults(t *testing.T) {
	oldLookup, oldDelay := lookupProductHost, productLookupRetryDelay
	clearHosts := func() {
		publicHosts.Lock()
		publicHosts.until = map[string]time.Time{}
		publicHosts.Unlock()
	}
	clearHosts()
	t.Cleanup(func() { lookupProductHost, productLookupRetryDelay = oldLookup, oldDelay; clearHosts() })
	productLookupRetryDelay = time.Millisecond
	calls := map[string]int{}
	lookupProductHost = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		calls[host]++
		switch host {
		case "flaky.example":
			if calls[host] < 3 {
				return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
			}
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		case "private.example":
			return []net.IPAddr{{IP: net.ParseIP("192.168.1.10")}}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if e := publicProductURL(ctx, "https://flaky.example/image"+fmt.Sprint(i)+".jpg"); e != nil {
			t.Fatal("transient DNS failure not retried", e)
		}
	}
	if calls["flaky.example"] != 3 {
		t.Fatal("public host should be resolved once and then cached", calls)
	}
	for i := 0; i < 2; i++ {
		if publicProductURL(ctx, "https://private.example/x") == nil {
			t.Fatal("private target allowed")
		}
	}
	if calls["private.example"] != 2 {
		t.Fatal("private answer must not be cached", calls)
	}
	if e := publicProductURL(ctx, "https://missing.example/x"); e == nil || !errors.Is(e, errProductTarget) || calls["missing.example"] != 3 {
		t.Fatal("persistent DNS failure should stop after three attempts", e, calls)
	}
}

// The page Amazon returns to some direct requests instead of the listing.
const amazonBlockPage = `<!doctype html><html lang="en"><head><title dir="ltr">Amazon.com</title></head><body>
<div class="a-container a-padding-double-large"><div class="a-row a-spacing-double-large">
<div class="a-box a-alert a-alert-info a-spacing-base"><div class="a-box-inner"><h4>Click the button below to continue shopping</h4></div></div>
<form method="get" action="/errors/validateCaptcha" name=""><input type=hidden name="amzn" value="x"/>
<button type="submit" class="a-button-text" alt="Continue shopping">Continue shopping</button></form>
</div><div class="a-divider-inner"></div><div class="a-text-center a-spacing-small a-size-mini"><a href="#">Conditions of Use</a> <a href="#">Privacy Policy</a></div>
<div class="a-text-center a-size-mini a-color-base">&copy; 1996-2025, Amazon.com, Inc. or its affiliates</div></div></body></html>`

func TestAmazonContinueShoppingPageUsesBrowser(t *testing.T) {
	server := productServer(t)
	for _, path := range []string{"/amazon-block", "/amazon-block-text"} {
		s := fakeBrowserService(t, "page", productPage(server.URL))
		info, e := s.fetchProduct(context.Background(), productRun(t, s, server.URL+path).ID)
		if e != nil || info.Title != "浏览器商品" || info.Error != "" {
			t.Fatal(path, info, e)
		}
		if !strings.Contains(logText(t, s), "返回人机验证页") {
			t.Fatal(path, "block page not recognised")
		}
	}
}

func TestBlockPageNeverBecomesProductData(t *testing.T) {
	server := productServer(t)
	// The browser also lands on the block page (for example the user did not click it).
	s := fakeBrowserService(t, "page", amazonBlockPage)
	info, e := s.fetchProduct(context.Background(), productRun(t, s, server.URL+"/amazon-block").ID)
	if e != nil || !strings.Contains(info.Error, "人机验证") || !strings.Contains(info.Error, "请上传图片或粘贴商品描述") {
		t.Fatal(info, e)
	}
	if info.Title != "" || info.Description != "" || len(info.ReferenceIDs) != 0 {
		t.Fatal("block page kept as product data", info)
	}
}
