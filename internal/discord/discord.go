// Package discord delivers product stock notifications through Discord webhooks.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"mediamarkt-monitor/internal/checker"
	productapi "mediamarkt-monitor/internal/product"
	"mediamarkt-monitor/internal/retry"
)

// Notifier serializes webhook requests and shares rate-limit state across
// workers. Construct it with New.
type Notifier struct {
	url      string
	client   *http.Client
	gate     chan struct{}
	nextSend time.Time
	disabled error
}

// New validates the webhook and creates a direct HTTP client that requests
// delivery confirmation and rejects redirects.
func New(webhook string) (*Notifier, error) {
	normalized, err := NormalizeWebhookURL(webhook)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	n := &Notifier{url: normalized, gate: make(chan struct{}, 1), client: &http.Client{
		Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	n.gate <- struct{}{}
	return n, nil
}

// NormalizeWebhookURL validates a Discord webhook without including credentials
// in errors, and adds delivery confirmation to its canonical query string.
func NormalizeWebhookURL(webhook string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(webhook))
	if err != nil || u == nil {
		return "", fmt.Errorf("invalid Discord webhook URL")
	}
	validHost := u.Hostname() == "discord.com" || u.Hostname() == "discordapp.com" || u.Hostname() == "canary.discord.com" || u.Hostname() == "ptb.discord.com"
	validPath := regexp.MustCompile(`^/api/(?:v[0-9]+/)?webhooks/[0-9]+/[A-Za-z0-9_-]+$`).MatchString(u.Path)
	if u.Scheme != "https" || !validHost || !validPath || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return "", fmt.Errorf("expected an HTTPS Discord webhook URL")
	}
	query := u.Query()
	query.Set("wait", "true")
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (n *Notifier) Close() { n.client.CloseIdleConnections() }

// Notify performs one attempt. Shared rate-limit state and a gate serialize
// webhook requests from different product workers. No product proxies are used.
func (n *Notifier) Notify(ctx context.Context, event checker.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := discordPayload(event)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-n.gate:
	}
	defer func() { n.gate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.disabled != nil {
		return n.disabled
	}
	if delay := time.Until(n.nextSend); delay > 0 {
		if err := retry.Wait(ctx, delay); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create Discord webhook request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// net/http errors include the credential-bearing URL; do not log it.
		return &checker.NotificationError{Message: "Discord webhook transport failure", Retryable: true}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	delay := retry.ParseAfter(resp.Header.Get("Retry-After"), time.Now())
	if resp.StatusCode == http.StatusTooManyRequests {
		var limit struct {
			RetryAfter json.Number `json:"retry_after"`
		}
		if json.Unmarshal(body, &limit) == nil {
			delay = max(delay, retry.ParseAfter(limit.RetryAfter.String(), time.Now()))
		}
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		delay = max(delay, retry.ParseAfter(resp.Header.Get("X-RateLimit-Reset-After"), time.Now()))
	}
	if delay > 0 {
		n.nextSend = time.Now().Add(delay)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// A confirmed success must not be retried just because its response body
		// was truncated or unreadable.
		return nil
	}
	failure := &checker.NotificationError{
		Message:    fmt.Sprintf("Discord webhook HTTP %d", resp.StatusCode),
		Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
		RetryAfter: delay,
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 {
		n.disabled = failure
	}
	return failure
}

func discordPayload(event checker.Event) ([]byte, error) {
	var product struct {
		Name   string          `json:"name"`
		URL    string          `json:"url"`
		Image  json.RawMessage `json:"image"`
		Offers json.RawMessage `json:"offers"`
	}
	if err := json.Unmarshal(event.Product, &product); err != nil {
		return nil, fmt.Errorf("decode Discord product data")
	}
	type offer struct {
		Availability  string          `json:"availability"`
		Price         json.RawMessage `json:"price"`
		PriceCurrency string          `json:"priceCurrency"`
	}
	var offers []offer
	rawOffers := bytes.TrimSpace(product.Offers)
	if len(rawOffers) > 0 && rawOffers[0] == '[' {
		json.Unmarshal(rawOffers, &offers)
	} else {
		var single offer
		if json.Unmarshal(rawOffers, &single) == nil {
			offers = []offer{single}
		}
	}
	price := "?"
	for _, item := range offers {
		if productapi.NormalizeAvailability(item.Availability) != "InStock" {
			continue
		}
		var text string
		if json.Unmarshal(item.Price, &text) != nil {
			text = string(item.Price)
		}
		if text != "" && text != "null" {
			price = text + " " + item.PriceCurrency
		}
		break
	}
	link := "https://www.mediamarkt." + event.Country + "/de/product/-" + event.PID + ".html"
	if u, err := url.Parse(product.URL); err == nil && u.Scheme == "https" && u.Host == "www.mediamarkt."+event.Country {
		link = product.URL
	}
	name := product.Name
	if name == "" {
		name = "MediaMarkt " + event.PID
	}
	embed := map[string]any{
		"title": truncateDiscordText("InStock: "+name, 256), "url": link, "color": 5763719,
		"timestamp": event.Time.UTC().Format(time.RFC3339),
		"fields": []map[string]any{
			{"name": "PID", "value": event.PID, "inline": true},
			{"name": "Region", "value": strings.ToUpper(event.Country), "inline": true},
			{"name": "Preis", "value": truncateDiscordText(price, 1024), "inline": true},
		},
	}
	var images []string
	if json.Unmarshal(product.Image, &images) != nil {
		var image string
		if json.Unmarshal(product.Image, &image) == nil && image != "" {
			images = []string{image}
		}
	}
	if len(images) > 0 {
		if u, err := url.Parse(images[0]); err == nil && u.Scheme == "https" && u.Hostname() != "" {
			embed["thumbnail"] = map[string]string{"url": images[0]}
		}
	}
	return json.Marshal(map[string]any{"embeds": []any{embed}, "allowed_mentions": map[string]any{"parse": []string{}}})
}

func truncateDiscordText(text string, limit int) string {
	units := 0
	for index, r := range text {
		units += utf16.RuneLen(r)
		if units > limit {
			return text[:index]
		}
	}
	return text
}
