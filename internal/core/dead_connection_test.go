package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// freezingRelay forwards TCP between the client and an HTTPS server until it is
// frozen; then it holds every byte but keeps both sockets open, like a proxy
// tunnel whose far side died without a FIN or RST reaching the client.
type freezingRelay struct {
	ln     net.Listener
	frozen atomic.Bool
}

func newFreezingRelay(t *testing.T, target string) *freezingRelay {
	t.Helper()
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	r := &freezingRelay{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			up, e := net.Dial("tcp", target)
			if e != nil {
				c.Close()
				continue
			}
			t.Cleanup(func() { c.Close(); up.Close() })
			go r.pipe(up, c)
			go r.pipe(c, up)
		}
	}()
	return r
}

func (r *freezingRelay) pipe(dst, src net.Conn) {
	buf := make([]byte, 32<<10)
	for {
		n, e := src.Read(buf)
		for n > 0 && r.frozen.Load() {
			time.Sleep(20 * time.Millisecond)
		}
		if n > 0 {
			if _, e := dst.Write(buf[:n]); e != nil {
				return
			}
		}
		if e != nil {
			dst.Close()
			return
		}
	}
}

// 11/18 (幻灵, through the system proxy): three images sent together waited 263 s
// without a byte while the platform showed them finished; the results were lost.
// All three were streams of one HTTP/2 connection whose tunnel had died silently,
// and nothing checked the connection, so they would have waited the full 10
// minutes. The connection must be found dead within a short time.
func TestDeadHTTP2ConnectionIsNoticedQuickly(t *testing.T) {
	oldAfter, oldTimeout := http2PingAfter, http2PingTimeout
	http2PingAfter, http2PingTimeout = time.Second, time.Second
	t.Cleanup(func() { http2PingAfter, http2PingTimeout = oldAfter, oldTimeout })

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/v1/images/edits" {
			time.Sleep(2 * time.Second) // generating
		}
		io.WriteString(w, `{"data":[]}`)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	relay := newFreezingRelay(t, server.Listener.Addr().String())

	transport := newNetworkTransport()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, ServerName: "example.com"}
	client := &http.Client{Transport: transport}
	url := "https://" + relay.ln.Addr().String()

	// A healthy connection that answers pings keeps working through a slow image.
	if resp, e := client.Get(url + "/v1/models"); e != nil || resp.ProtoMajor != 2 {
		t.Fatalf("warm-up: %v %v", resp, e)
	} else {
		resp.Body.Close()
	}
	if resp, e := client.Post(url+"/v1/images/edits", "application/json", nil); e != nil {
		t.Fatalf("slow image on a healthy connection: %v", e)
	} else {
		resp.Body.Close()
	}

	// Three images on the same connection; then the tunnel dies silently.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(ctx, "POST", url+"/v1/images/edits", nil)
			resp, e := client.Do(req)
			if e == nil {
				resp.Body.Close()
			}
			errs[i] = e
		}()
	}
	time.Sleep(500 * time.Millisecond)
	relay.frozen.Store(true)
	wg.Wait()
	took := time.Since(start)
	for i, e := range errs {
		if e == nil || ctx.Err() != nil {
			t.Fatalf("image %d on the dead connection: err=%v after %s (still waiting when the test gave up)", i+1, e, took.Round(time.Second))
		}
	}
	if took > 8*time.Second {
		t.Fatalf("dead connection noticed only after %s", took.Round(time.Second))
	}
}
