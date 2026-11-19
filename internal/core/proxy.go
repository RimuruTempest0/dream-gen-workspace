package core

import (
	"errors"
	"golang.org/x/net/http/httpproxy"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var errProxyAddress = errors.New("system proxy address format is not supported")

// Keep explicit environment configuration first. Normal Explorer launches do
// not inherit terminal-only proxy variables, so use Windows' static system
// proxy as a fallback instead of silently bypassing the user's network route.
func networkProxy() func(*http.Request) (*url.URL, error) {
	server, bypass := systemProxyConfig()
	return proxyResolver(server, bypass)
}

// Requests to one host share an HTTP/2 connection, and an image waits up to
// minutes on it without a byte. When the tunnel under it dies silently (a proxy
// node dropped, no FIN or RST reaching us), every image on it waited the full
// 10-minute limit while the service had long finished (11/18: 263 s, results
// lost). A connection silent for http2PingAfter is pinged; no answer within
// http2PingTimeout closes it, so the images fail and are retried at once. A
// live service answers pings while it generates.
var (
	http2PingAfter   = 20 * time.Second
	http2PingTimeout = 15 * time.Second
)

func newNetworkTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = networkProxy()
	transport.HTTP2 = &http.HTTP2Config{SendPingTimeout: http2PingAfter, PingTimeout: http2PingTimeout}
	return transport
}

func proxyResolver(server, bypass string) func(*http.Request) (*url.URL, error) {
	return func(r *http.Request) (*url.URL, error) {
		host := r.URL.Hostname()
		if strings.EqualFold(host, "localhost") {
			return nil, nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil, nil
		}
		envName := "HTTP_PROXY"
		if r.URL.Scheme == "https" {
			envName = "HTTPS_PROXY"
		}
		if os.Getenv(envName) != "" || os.Getenv(strings.ToLower(envName)) != "" {
			return http.ProxyFromEnvironment(r)
		}
		if proxyBypassed(host, r.URL.Host, bypass) {
			return nil, nil
		}
		u, e := proxyURL(server, r.URL.Scheme)
		if e != nil || u == nil {
			return u, e
		}
		noProxy := os.Getenv("NO_PROXY")
		if noProxy == "" {
			noProxy = os.Getenv("no_proxy")
		}
		return (&httpproxy.Config{HTTPProxy: u.String(), HTTPSProxy: u.String(), NoProxy: noProxy}).ProxyFunc()(r.URL)
	}
}

func proxyURL(server, scheme string) (*url.URL, error) {
	raw := strings.TrimSpace(server)
	if strings.Contains(raw, "=") {
		raw = ""
		for _, part := range strings.Split(server, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
			if ok && strings.EqualFold(k, scheme) {
				raw = strings.TrimSpace(v)
				break
			}
		}
	}
	if raw == "" {
		return nil, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, e := url.Parse(raw)
	if e != nil {
		return nil, e
	}
	if u.Host == "" || !(u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "socks5" || u.Scheme == "socks5h") {
		return nil, &url.Error{Op: "proxy", URL: "[system proxy]", Err: errProxyAddress}
	}
	return u, nil
}

func proxyBypassed(host, hostPort, list string) bool {
	for _, part := range strings.Split(list, ";") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p == "" {
			continue
		}
		if p == "<local>" {
			if !strings.Contains(host, ".") && !strings.Contains(host, ":") {
				return true
			}
			continue
		}
		pattern := "(?i)^" + strings.ReplaceAll(strings.ReplaceAll(regexp.QuoteMeta(p), `\*`, ".*"), `\?`, ".") + "$"
		re, e := regexp.Compile(pattern)
		if e == nil && (re.MatchString(host) || re.MatchString(hostPort)) {
			return true
		}
	}
	return false
}
