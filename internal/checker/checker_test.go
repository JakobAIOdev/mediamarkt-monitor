package checker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	productapi "mediamarkt-monitor/internal/product"
	"mediamarkt-monitor/internal/retry"
)

func availabilityJSON(status string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"sku":"2087300","offers":{"availability":%q}}`, status))
}

func TestCheckerStopsOnlyAtInStock(t *testing.T) {
	statuses := []string{"https://schema.org/OutOfStock", "https://schema.org/PreOrder", "BackOrder", "Unknown", "https://schema.org/InStock"}
	var events []Event
	var delays []time.Duration
	calls := 0
	err := checkUntilInStock(context.Background(), 30*time.Second,
		func(context.Context) (json.RawMessage, error) {
			if calls >= len(statuses) {
				t.Fatal("checker continued after InStock")
			}
			product := availabilityJSON(statuses[calls])
			calls++
			return product, nil
		}, func(event Event) error {
			events = append(events, event)
			return nil
		}, func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		})
	if err != nil || calls != len(statuses) || len(delays) != len(statuses)-1 {
		t.Fatalf("err=%v, calls=%d, delays=%v", err, calls, delays)
	}
	for i, event := range events {
		if i < len(events)-1 && (event.Event != "checked" || len(event.Product) != 0) {
			t.Fatalf("unexpected event before InStock: %+v", event)
		}
	}
	last := events[len(events)-1]
	if last.Event != "in_stock" || last.Availability != "InStock" || !json.Valid(last.Product) || last.RetryIn != "" {
		t.Fatalf("invalid InStock event: %+v", last)
	}
}

func TestCheckerRetriesErrorsAndResetsBackoff(t *testing.T) {
	var delays []time.Duration
	var events []Event
	calls := 0
	err := checkUntilInStock(context.Background(), 30*time.Second,
		func(context.Context) (json.RawMessage, error) {
			calls++
			switch calls {
			case 1:
				return nil, &productapi.HTTPError{PID: "2087300", StatusCode: 403}
			case 2:
				return nil, fmt.Errorf("request failed: %w", &productapi.HTTPError{PID: "2087300", StatusCode: 429, RetryAfter: 10 * time.Minute})
			case 3:
				return availabilityJSON("OutOfStock"), nil
			case 4:
				return json.RawMessage(`{"offers":{}}`), nil
			case 5:
				return availabilityJSON("InStock"), nil
			default:
				t.Fatal("unexpected extra check")
				return nil, nil
			}
		}, func(event Event) error {
			events = append(events, event)
			return nil
		}, func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		})
	want := []time.Duration{time.Minute, 10 * time.Minute, 30 * time.Second, time.Minute}
	if err != nil || !reflect.DeepEqual(delays, want) {
		t.Fatalf("err=%v, delays=%v, want=%v", err, delays, want)
	}
	for _, i := range []int{0, 1, 3} {
		if events[i].Event != "error" || events[i].Error == "" || events[i].Availability != "" {
			t.Fatalf("fetch/parse failure was not reported as an error: %+v", events[i])
		}
	}
}

func TestCheckerCancellationDuringWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err := checkUntilInStock(ctx, time.Hour,
		func(context.Context) (json.RawMessage, error) {
			calls++
			return availabilityJSON("OutOfStock"), nil
		}, func(Event) error {
			cancel()
			return nil
		}, retry.Wait)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v, calls=%d", err, calls)
	}
}

func TestCheckerStopsOnOutputFailure(t *testing.T) {
	want := errors.New("output closed")
	err := checkUntilInStock(context.Background(), time.Second,
		func(context.Context) (json.RawMessage, error) { return availabilityJSON("InStock"), nil },
		func(Event) error { return want },
		func(context.Context, time.Duration) error { t.Fatal("unexpected wait"); return nil })
	if !errors.Is(err, want) {
		t.Fatalf("err=%v, want=%v", err, want)
	}
}

func TestRetryDelay(t *testing.T) {
	for i, want := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute} {
		if got := retryDelay(30*time.Second, i+1, 0); got != want {
			t.Fatalf("failure %d: got %v, want %v", i+1, got, want)
		}
	}
	if got := retryDelay(10*time.Minute, 20, 0); got != 10*time.Minute {
		t.Fatalf("retry shortened configured interval: %v", got)
	}
}
