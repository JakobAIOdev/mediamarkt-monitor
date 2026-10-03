package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mediamarkt-monitor/internal/checker"
)

func discordTestEvent() checker.Event {
	return checker.Event{Time: time.Now(), PID: "2087300", Country: "at", Event: "in_stock", Product: json.RawMessage(`{"name":"Pokémon Box","image":["https://assets.mmsrg.com/image.png"],"offers":[{"availability":"OutOfStock","price":99,"priceCurrency":"EUR"},{"availability":"https://schema.org/InStock","price":59.99,"priceCurrency":"EUR"}]}`)}
}

func testDiscordNotifier(server *httptest.Server) *Notifier {
	n := &Notifier{url: server.URL + "/api/webhooks/123/test-token?wait=true", client: server.Client(), gate: make(chan struct{}, 1)}
	n.gate <- struct{}{}
	return n
}

func TestDiscordWebhookPayloadAndRateLimitRetry(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Query().Get("wait") != "true" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("wrong webhook request: %s %s", r.Method, r.URL.Path)
		}
		var payload struct {
			Embeds []struct {
				Title  string                         `json:"title"`
				URL    string                         `json:"url"`
				Fields []struct{ Name, Value string } `json:"fields"`
			} `json:"embeds"`
			AllowedMentions struct {
				Parse []string `json:"parse"`
			} `json:"allowed_mentions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		} else if len(payload.Embeds) != 1 || payload.Embeds[0].Title != "InStock: Pokémon Box" || payload.Embeds[0].URL != "https://www.mediamarkt.at/de/product/-2087300.html" || len(payload.Embeds[0].Fields) != 3 || payload.Embeds[0].Fields[2].Value != "59.99 EUR" || payload.AllowedMentions.Parse == nil || len(payload.AllowedMentions.Parse) != 0 {
			t.Errorf("unexpected payload: %+v", payload)
		}
		if requests.Add(1) == 1 {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"retry_after":0.001}`)
			return
		}
		fmt.Fprint(w, `{"id":"confirmed"}`)
	}))
	defer server.Close()
	n := testDiscordNotifier(server)
	var delays []time.Duration
	err := checker.NotifyUntilSent(context.Background(), discordTestEvent(), n,
		func(event checker.Event) error {
			if event.Event != "notification_retry" {
				t.Errorf("unexpected event: %+v", event)
			}
			return nil
		}, func(_ context.Context, delay time.Duration) error { delays = append(delays, delay); return nil })
	if err != nil || requests.Load() != 2 || len(delays) != 1 || delays[0] < time.Millisecond {
		t.Fatalf("err=%v, requests=%d, delays=%v", err, requests.Load(), delays)
	}
}

func TestDeletedWebhookIsNotRequestedAgain(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(404)
	}))
	defer server.Close()
	n := testDiscordNotifier(server)
	for i := 0; i < 2; i++ {
		err := n.Notify(context.Background(), discordTestEvent())
		var failure *checker.NotificationError
		if !errors.As(err, &failure) || failure.Retryable || strings.Contains(err.Error(), "test-token") {
			t.Fatalf("invalid permanent failure: %v", err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("deleted webhook requested %d times", requests.Load())
	}
}

func TestDiscordGateCanBeCancelled(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		close(entered)
		<-release
		fmt.Fprint(w, `{"id":"confirmed"}`)
	}))
	defer server.Close()
	n := testDiscordNotifier(server)
	first := make(chan error, 1)
	go func() { first <- n.Notify(context.Background(), discordTestEvent()) }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := n.Notify(ctx, discordTestEvent())
	close(release)
	if firstErr := <-first; firstErr != nil {
		t.Fatal(firstErr)
	}
	if !errors.Is(err, context.Canceled) || requests.Load() != 1 {
		t.Fatalf("err=%v, requests=%d", err, requests.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestWebhookURLValidationAndSecretRedaction(t *testing.T) {
	n, err := New("https://discord.com/api/webhooks/123/secret-token")
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	n.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("failed URL %s", req.URL)
	})
	if err := n.Notify(context.Background(), discordTestEvent()); err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("webhook token leaked: %v", err)
	}
	for _, value := range []string{"http://discord.com/api/webhooks/123/token", "https://discord.com.evil.example/api/webhooks/123/token", "https://discord.com/api/webhooks/123/token#fragment", "https://discord.com/not-a-webhook"} {
		if _, err := New(value); err == nil {
			t.Errorf("accepted invalid URL %q", value)
		}
	}
	// Do not retry a successful response whose message body could not be read.
	n.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(errorReader{})}, nil
	})
	if err := n.Notify(context.Background(), discordTestEvent()); err != nil {
		t.Fatal(err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("connection closed") }

func TestDiscordTextLimitPreservesUnicode(t *testing.T) {
	if got := truncateDiscordText("a😀b", 3); got != "a😀" {
		t.Fatalf("got %q", got)
	}
}

func TestMonitorAlertsDoNotRequireProductData(t *testing.T) {
	for _, kind := range []string{"monitor_warning", "monitor_recovered", "monitor_failed"} {
		event := checker.Event{Time: time.Now(), PID: "2087300", Country: "at", Event: kind, Error: "proxy file contains no proxies", ConsecutiveErrors: 5, RetryIn: "5m0s"}
		payload, err := discordPayload(event)
		if err != nil {
			t.Fatal(err)
		}
		var message struct {
			Embeds []struct {
				Title  string
				Fields []struct{ Name, Value string }
			}
			AllowedMentions struct{ Parse []string } `json:"allowed_mentions"`
		}
		if err := json.Unmarshal(payload, &message); err != nil || len(message.Embeds) != 1 || len(message.Embeds[0].Fields) != 4 || len(message.AllowedMentions.Parse) != 0 || message.AllowedMentions.Parse == nil {
			t.Fatalf("invalid %s alert: %s (%v)", kind, payload, err)
		}
	}
}
