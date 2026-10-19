package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSystemProxyAddressAndBypass(t *testing.T) {
	for _, tc := range []struct{ server, scheme, want string }{
		{"127.0.0.1:7890", "https", "http://127.0.0.1:7890"},
		{"http=127.0.0.1:8001;https=127.0.0.1:8002", "https", "http://127.0.0.1:8002"},
		{"http=127.0.0.1:8001", "https", ""},
		{"socks5://127.0.0.1:1080", "http", "socks5://127.0.0.1:1080"},
	} {
		u, e := proxyURL(tc.server, tc.scheme)
		actual := ""
		if u != nil {
			actual = u.String()
		}
		if e != nil || actual != tc.want {
			t.Fatalf("proxy mapping %s: %s %v", tc.server, actual, e)
		}
	}
	for _, tc := range []struct {
		host, list string
		want       bool
	}{
		{"printer", "<local>", true}, {"example.com", "<local>", false}, {"a.local", "*.local;127.*", true}, {"example.com", "*.local;127.*", false}, {"127.0.0.1", "127.*", true},
	} {
		if proxyBypassed(tc.host, tc.host, tc.list) != tc.want {
			t.Fatalf("incorrect proxy bypass for %s", tc.host)
		}
	}
}

func TestSystemProxyFallbackWithoutEnvironment(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"} {
		t.Setenv(name, "")
	}
	hits := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Host != "image-provider.invalid" || r.Method != "POST" {
			t.Errorf("request did not reach configured proxy: %s", r.URL)
		}
		io.WriteString(w, `{"data":[{"url":"https://example.com/result.png"}]}`)
	}))
	defer proxy.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxyResolver(proxy.URL, "<local>")
	defer transport.CloseIdleConnections()
	provider := &Provider{Client: &http.Client{Transport: transport}}
	var diag GenerationDiagnostic
	outputs, unknown, err := provider.GenerateWithProgress(context.Background(), Task{Connection: Connection{BaseURL: "http://image-provider.invalid"}}, "mock-key", nil, "", func(d GenerationDiagnostic) { diag = d })
	if err != nil || unknown || len(outputs) != 1 || hits != 1 || diag.NetworkRoute != "proxy" {
		t.Fatalf("system proxy fallback failed: %v %+v", err, diag)
	}
	local, _ := http.NewRequest("GET", "http://127.0.0.1:8080/models", nil)
	u, e := transport.Proxy(local)
	if e != nil || u != nil {
		t.Fatal("localhost must stay direct")
	}
	t.Setenv("NO_PROXY", "image-provider.invalid")
	bypassed, _ := http.NewRequest("GET", "http://image-provider.invalid/models", nil)
	u, e = transport.Proxy(bypassed)
	if e != nil || u != nil {
		t.Fatal("explicit NO_PROXY bypass was ignored")
	}
}
