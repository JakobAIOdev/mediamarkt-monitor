package checker

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	tls_client "github.com/bogdanfinn/tls-client"

	productapi "mediamarkt-monitor/internal/product"
	taskconfig "mediamarkt-monitor/internal/tasks"
)

type closeTrackingClient struct {
	tls_client.HttpClient
	closed int
}

func (c *closeTrackingClient) CloseIdleConnections() { c.closed++ }

func TestWorkerReusesClientAndRotatesAfterFailure(t *testing.T) {
	proxies := []string{"http://proxy1:8080", "http://proxy2:8080"}
	f := newProductFetcher(taskconfig.Task{PID: "2087300", Region: "at"}, proxies, 1)
	var created []string
	var clients []*closeTrackingClient
	f.create = func(proxy string) (tls_client.HttpClient, error) {
		created = append(created, proxy)
		c := &closeTrackingClient{}
		clients = append(clients, c)
		return c, nil
	}
	calls := 0
	f.get = func(_ context.Context, _ tls_client.HttpClient, region, pid string) (json.RawMessage, error) {
		if region != "at" || pid != "2087300" {
			t.Fatalf("wrong task: %s/%s", region, pid)
		}
		calls++
		if calls == 1 {
			return nil, &productapi.HTTPError{StatusCode: 403}
		}
		return availabilityJSON("OutOfStock"), nil
	}
	if _, err := f.Fetch(context.Background()); err == nil {
		t.Fatal("expected first request to fail")
	}
	for i := 0; i < 2; i++ {
		if _, err := f.Fetch(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	if !reflect.DeepEqual(created, []string{proxies[1], proxies[0]}) || clients[0].closed != 1 || clients[1].closed != 1 {
		t.Fatalf("unexpected proxy/client lifecycle: created=%v, clients=%+v", created, clients)
	}
}
