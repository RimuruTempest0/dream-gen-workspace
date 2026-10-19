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
)

var errProxyAddress = errors.New("系统代理地址格式不受支持")

// Keep explicit environment configuration first. Normal Explorer launches do
// not inherit terminal-only proxy variables, so use Windows' static system
// proxy as a fallback instead of silently bypassing the user's network route.
func networkProxy() func(*http.Request) (*url.URL, error) {
	server, bypass := systemProxyConfig()
	return proxyResolver(server, bypass)
}

func newNetworkTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = networkProxy()
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
		return nil, &url.Error{Op: "proxy", URL: "[系统代理]", Err: errProxyAddress}
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
