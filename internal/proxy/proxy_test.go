package proxy

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"mediamarkt-monitor/internal/product"
)

func TestProxyFormats(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"proxy.example:8080", "http://proxy.example:8080"},
		{"proxy.example:8080:user:p@ss:word", "http://user:p%40ss%3Aword@proxy.example:8080"},
		{"http://user:pass@proxy.example:8080", "http://user:pass@proxy.example:8080"},
		{"socks5://[::1]:1080", "socks5://[::1]:1080"},
	} {
		got, err := normalizeProxy(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, err=%v, want=%q", tc.input, got, err, tc.want)
		}
	}
	for _, input := range []string{"proxy:0", "proxy:65536", "ftp://user:secret@proxy:80", "http://user:secret@proxy", "http://proxy:80/path"} {
		if _, err := normalizeProxy(input); err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("invalid proxy error leaked credentials or was nil: %v", err)
		}
	}
	proxies, err := readProxies(strings.NewReader("# comment\n\nproxy.example:8080\nproxy2.example:8081:user:password\n"))
	if err != nil || len(proxies) != 2 {
		t.Fatalf("proxies=%v, err=%v", proxies, err)
	}
	if _, err := readProxies(strings.NewReader("# empty")); err == nil {
		t.Fatal("empty proxy file accepted")
	}
}

func TestProxyErrorRedactionPreservesRetryAfter(t *testing.T) {
	status := &product.HTTPError{StatusCode: 429}
	err := RedactError(fmt.Errorf("http://user:secret@proxy:8080 user:secret secret: %w", status), "http://user:secret@proxy:8080")
	var got *product.HTTPError
	if strings.Contains(err.Error(), "secret") || !errors.As(err, &got) || got != status {
		t.Fatalf("redaction leaked credentials or lost underlying error: %v", err)
	}
}
