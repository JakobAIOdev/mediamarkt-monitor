package product

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
)

func TestTLSClientUsesAuthenticatedProxy(t *testing.T) {
	var connects, targetRequests atomic.Int32
	var authenticated atomic.Bool
	target := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		targetRequests.Add(1)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer target.Close()
	proxy := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodConnect {
			nethttp.Error(w, "CONNECT required", 405)
			return
		}
		connects.Add(1)
		authenticated.Store(r.Header.Get("Proxy-Authorization") == "Basic "+base64.StdEncoding.EncodeToString([]byte("user:password")))
		upstream, err := net.Dial("tcp", r.Host)
		if err != nil {
			nethttp.Error(w, "upstream failed", 502)
			return
		}
		defer upstream.Close()
		conn, buffered, err := w.(nethttp.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		done := make(chan struct{})
		go func() {
			io.Copy(upstream, buffered)
			upstream.Close()
			close(done)
		}()
		io.Copy(conn, upstream)
		conn.Close()
		<-done
	}))
	defer proxy.Close()
	u, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword("user", "password")
	client, err := NewClient(u.String(), tls_client.WithDisableHttp3())
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodGet, target.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != `{"ok":true}` || connects.Load() != 1 || targetRequests.Load() != 1 || !authenticated.Load() {
		t.Fatalf("proxy connection failed: body=%s err=%v connects=%d target=%d authenticated=%v", body, err, connects.Load(), targetRequests.Load(), authenticated.Load())
	}
}

func TestTLSClientDoesNotFallBackWhenProxyRejects(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		targetRequests.Add(1)
	}))
	defer target.Close()
	proxy := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.WriteHeader(407)
	}))
	defer proxy.Close()
	client, err := NewClient(proxy.URL, tls_client.WithDisableHttp3())
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodGet, target.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || targetRequests.Load() != 0 {
		t.Fatalf("expected proxy error without direct request: err=%v target=%d", err, targetRequests.Load())
	}
}
