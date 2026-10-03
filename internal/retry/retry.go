// Package retry provides HTTP retry-delay parsing and cancellable waits.
package retry

import (
	"context"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
)

// ParseAfter accepts seconds or an HTTP date and returns zero for invalid or
// expired values. Fractional seconds support notification rate-limit headers.
func ParseAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := time.ParseDuration(value + "s"); err == nil && seconds > 0 {
		return seconds
	}
	if deadline, err := http.ParseTime(value); err == nil && deadline.After(now) {
		return deadline.Sub(now)
	}
	return 0
}

// Wait pauses until the delay expires or the context is cancelled.
func Wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
