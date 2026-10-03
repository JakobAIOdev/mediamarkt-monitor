package checker

import (
	"context"
	"encoding/json"

	tls_client "github.com/bogdanfinn/tls-client"

	productapi "mediamarkt-monitor/internal/product"
	proxylist "mediamarkt-monitor/internal/proxy"
	taskconfig "mediamarkt-monitor/internal/tasks"
)

// A fetcher belongs to exactly one worker, so its client and cookie jar are never
// shared across PIDs or moved to another proxy after an error.
type productFetcher struct {
	task    taskconfig.Task
	proxies []string
	index   int
	client  tls_client.HttpClient
	create  func(string) (tls_client.HttpClient, error)
	get     func(context.Context, tls_client.HttpClient, string, string) (json.RawMessage, error)
}

func newProductFetcher(task taskconfig.Task, proxies []string, worker int) *productFetcher {
	index := 0
	if len(proxies) > 0 {
		index = worker % len(proxies)
	}
	return &productFetcher{task: task, proxies: proxies, index: index,
		create: func(proxy string) (tls_client.HttpClient, error) {
			return productapi.NewClient(proxy, tls_client.WithDisableHttp3())
		}, get: productapi.Get}
}

func (f *productFetcher) Fetch(ctx context.Context) (json.RawMessage, error) {
	proxy := ""
	if len(f.proxies) > 0 {
		proxy = f.proxies[f.index]
	}
	var err error
	if f.client == nil {
		f.client, err = f.create(proxy)
	}
	var product json.RawMessage
	if err == nil {
		product, err = f.get(ctx, f.client, f.task.Region, f.task.PID)
	}
	if err != nil {
		f.Close()
		if len(f.proxies) > 0 {
			f.index = (f.index + 1) % len(f.proxies)
		}
		return nil, proxylist.RedactError(err, proxy)
	}
	return product, nil
}

func (f *productFetcher) Close() {
	if f.client != nil {
		f.client.CloseIdleConnections()
		f.client = nil
	}
}
