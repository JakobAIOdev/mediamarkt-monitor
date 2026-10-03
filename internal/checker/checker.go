// Package checker runs independent product workers and coordinates stock events
// and notification retries. Notification transports implement Notifier.
package checker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	productapi "mediamarkt-monitor/internal/product"
)

// Event is a structured update emitted by a worker. Stock, snapshot and permanent
// notification failure events can include the full product data.
type Event struct {
	Time              time.Time       `json:"time"`
	PID               string          `json:"pid"`
	Country           string          `json:"country"`
	Event             string          `json:"event"`
	Attempt           int             `json:"attempt"`
	Availability      string          `json:"availability,omitempty"`
	Error             string          `json:"error,omitempty"`
	RetryIn           string          `json:"retry_in,omitempty"`
	Product           json.RawMessage `json:"product,omitempty"`
	ConsecutiveErrors int             `json:"consecutive_errors,omitempty"`
	NotificationFor   string          `json:"notification_for,omitempty"`
}

func checkUntilInStock(ctx context.Context, interval time.Duration,
	fetch func(context.Context) (json.RawMessage, error), emit func(Event) error,
	wait func(context.Context, time.Duration) error) error {
	if interval <= 0 {
		return fmt.Errorf("interval must be greater than zero")
	}
	failures := 0
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		product, err := fetch(ctx)
		var availability string
		var inStock bool
		if err == nil {
			availability, inStock, err = productapi.Availability(product)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		event := Event{Time: time.Now().UTC(), Attempt: attempt, Availability: availability}
		delay := interval
		if err != nil {
			failures++
			event.Event, event.Error = "error", err.Error()
			var retryAfter time.Duration
			var statusErr *productapi.HTTPError
			if errors.As(err, &statusErr) {
				retryAfter = statusErr.RetryAfter
			}
			delay = retryDelay(interval, failures, retryAfter)
		} else {
			failures = 0
			event.Event = "checked"
			if inStock {
				event.Event, event.Product = "in_stock", product
			}
		}
		if !inStock {
			event.RetryIn = delay.String()
		}
		if err := emit(event); err != nil {
			return fmt.Errorf("write checker event: %w", err)
		}
		if inStock {
			return nil
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
}

func retryDelay(interval time.Duration, failures int, retryAfter time.Duration) time.Duration {
	delay := max(interval, time.Minute)
	limit := max(delay, 5*time.Minute)
	for i := 1; i < failures && delay < limit; i++ {
		if delay >= limit/2 {
			delay = limit
		} else {
			delay *= 2
		}
	}
	return max(delay, retryAfter)
}
