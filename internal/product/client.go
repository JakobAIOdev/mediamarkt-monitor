package product

import (
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// NewClient creates a Chrome TLS client with an isolated cookie jar. Proxy
// clients disable HTTP/3 so requests use the configured proxy transport.
func NewClient(proxyURL string, options ...tls_client.HttpClientOption) (tls_client.HttpClient, error) {
	defaults := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(profiles.Chrome_150),
		tls_client.WithNotFollowRedirects(),
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
	}
	if proxyURL != "" {
		options = append(options, tls_client.WithProxyUrl(proxyURL), tls_client.WithDisableHttp3())
	}
	return tls_client.NewHttpClient(tls_client.NewNoopLogger(), append(defaults, options...)...)
}
