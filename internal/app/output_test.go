package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"mediamarkt-monitor/internal/checker"
	"mediamarkt-monitor/internal/tasks"
)

func TestPrettyOutputKeepsRemoteContentOnOneLineAndHidesConfigSecrets(t *testing.T) {
	var output bytes.Buffer
	r := newReporter(&output, "auto", true)
	taskList := []tasks.Task{{PID: "2087300", Region: "at", WebhookURL: "secret-token"}}
	if err := r.Start(taskList, 2, 30*time.Second, 5, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "secret-token") || !strings.Contains(output.String(), "2 proxies") {
		t.Fatalf("unsafe or missing startup summary: %q", output.String())
	}
	output.Reset()
	if err := r.Event(checker.Event{Time: time.Now(), PID: "2087300", Country: "at", Event: "in_stock", Product: json.RawMessage(`{"name":"Pokémon\nFake log\u001b[2J"}`)}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "\n") != 1 || strings.ContainsRune(output.String(), '\x1b') || !strings.Contains(output.String(), "IN STOCK") {
		t.Fatalf("unsafe terminal output: %q", output.String())
	}
}

func TestJSONOutputAndSingleProductRemainMachineReadable(t *testing.T) {
	var output bytes.Buffer
	r := newReporter(&output, "json", true)
	if err := r.Start(nil, 1, time.Second, 5, nil); err != nil {
		t.Fatal(err)
	}
	event := checker.Event{Time: time.Now(), PID: "1", Event: "in_stock", Product: json.RawMessage(`{"name":"Box","offers":{"price":59.99}}`)}
	if err := r.Event(event); err != nil {
		t.Fatal(err)
	}
	if err := r.Finish(nil); err != nil {
		t.Fatal(err)
	}
	var got checker.Event
	if err := json.Unmarshal(output.Bytes(), &got); err != nil || string(got.Product) != string(event.Product) {
		t.Fatalf("JSON mode must contain only the full event: %q, %v", output.String(), err)
	}
	output.Reset()
	r = newReporter(&output, "auto", false)
	if err := r.Product(event.Product); err != nil {
		t.Fatal(err)
	}
	var product map[string]any
	if err := json.Unmarshal(output.Bytes(), &product); err != nil || product["name"] != "Box" {
		t.Fatalf("invalid snapshot: %q", output.String())
	}
}

type failureNotifier func(context.Context, checker.Event) error

func (f failureNotifier) Notify(ctx context.Context, event checker.Event) error { return f(ctx, event) }

func TestFailureAlertWithoutProductAndCancellation(t *testing.T) {
	failure := errors.New("proxy file contains no proxies")
	calls := 0
	notifier := failureNotifier(func(_ context.Context, event checker.Event) error {
		calls++
		if event.Event != "monitor_failed" || event.Error != failure.Error() || len(event.Product) != 0 {
			t.Fatalf("wrong failure event: %+v", event)
		}
		return nil
	})
	if err := deliverFailure(context.Background(), notifier, failure); err != nil || calls != 1 {
		t.Fatalf("calls=%d, err=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := deliverFailure(ctx, notifier, failure); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancelled alert sent: calls=%d, err=%v", calls, err)
	}
}
