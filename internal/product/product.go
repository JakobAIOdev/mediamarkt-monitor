// Package product retrieves MediaMarkt JSON-LD using PID-only URLs and evaluates
// the published offer availability.
package product

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"golang.org/x/net/html"

	"mediamarkt-monitor/internal/retry"
)

const productUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"

// Get resolves the PID-only URL and returns the page's JSON-LD Product.
// This contains the published price and availability, rather than live store stock.
func Get(ctx context.Context, client tls_client.HttpClient, country, pid string) (json.RawMessage, error) {
	country, pid, err := ValidateInput(country, pid)
	if err != nil {
		return nil, err
	}
	domain := "www.mediamarkt." + country
	target := "https://" + domain + "/de/product/-" + pid + ".html"
	for redirects := 0; redirects <= 5; redirects++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, fmt.Errorf("create product request: %w", err)
		}
		req.Header = http.Header{
			"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
			"accept-language":           {"de-DE,de;q=0.9,en;q=0.8"},
			"user-agent":                {productUserAgent},
			"sec-ch-ua":                 {`"Google Chrome";v="150", "Not_A Brand";v="8", "Chromium";v="150"`},
			"sec-ch-ua-mobile":          {"?0"},
			"sec-ch-ua-platform":        {`"macOS"`},
			"sec-fetch-dest":            {"document"},
			"sec-fetch-mode":            {"navigate"},
			"sec-fetch-site":            {"none"},
			"sec-fetch-user":            {"?1"},
			"upgrade-insecure-requests": {"1"},
			"cache-control":             {"no-cache"},
			"pragma":                    {"no-cache"},
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request product %s: %w", pid, err)
		}
		switch resp.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			next, err := resp.Location()
			resp.Body.Close()
			if err != nil {
				return nil, fmt.Errorf("resolve product redirect: %w", err)
			}
			if next.Scheme != "https" || next.Host != domain {
				return nil, fmt.Errorf("unexpected product redirect: %s", next)
			}
			target = next.String()
			continue
		case http.StatusOK:
			const maxPageBytes = 8 << 20
			body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
			resp.Body.Close()
			if err != nil {
				return nil, fmt.Errorf("read product page: %w", err)
			}
			if len(body) > maxPageBytes {
				return nil, fmt.Errorf("product page exceeds 8 MiB")
			}
			return extractProduct(strings.NewReader(string(body)), pid)
		default:
			retryAfter := retry.ParseAfter(resp.Header.Get("Retry-After"), time.Now())
			resp.Body.Close()
			return nil, &HTTPError{PID: pid, StatusCode: resp.StatusCode, RetryAfter: retryAfter}
		}
	}
	return nil, fmt.Errorf("too many product redirects")
}

// ValidateInput normalizes the region and PID and rejects unsupported values.
func ValidateInput(country, pid string) (string, string, error) {
	country = strings.ToLower(strings.TrimSpace(country))
	if country != "at" && country != "de" {
		return "", "", fmt.Errorf("unsupported country %q: use at or de", country)
	}
	pid = strings.TrimSpace(pid)
	if pid == "" || strings.IndexFunc(pid, func(r rune) bool { return r < '0' || r > '9' }) != -1 {
		return "", "", fmt.Errorf("PID must contain only digits")
	}
	return country, pid, nil
}

func extractProduct(page io.Reader, pid string) (json.RawMessage, error) {
	tokens := html.NewTokenizer(page)
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			if err := tokens.Err(); err != io.EOF {
				return nil, fmt.Errorf("parse product page: %w", err)
			}
			return nil, fmt.Errorf("no JSON-LD product for PID %s", pid)
		case html.StartTagToken:
			tag := tokens.Token()
			if tag.Data != "script" {
				continue
			}
			isJSON := false
			for _, attr := range tag.Attr {
				if attr.Key == "type" && attr.Val == "application/ld+json" {
					isJSON = true
				}
			}
			if !isJSON || tokens.Next() != html.TextToken {
				continue
			}
			data := append(json.RawMessage(nil), tokens.Text()...)
			var record struct {
				Type   string          `json:"@type"`
				SKU    string          `json:"sku"`
				Object json.RawMessage `json:"object"`
			}
			if err := json.Unmarshal(data, &record); err != nil {
				continue
			}
			if record.Type == "BuyAction" {
				data = record.Object
				if err := json.Unmarshal(data, &record); err != nil {
					continue
				}
			}
			if record.Type == "Product" && record.SKU == pid {
				return data, nil
			}
		}
	}
}
