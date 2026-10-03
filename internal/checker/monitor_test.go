package checker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	taskconfig "mediamarkt-monitor/internal/tasks"
)

func TestTaskWorkersRunConcurrentlyAndIndependently(t *testing.T) {
	tasks := []taskconfig.Task{{PID: "1", Region: "at"}, {PID: "2", Region: "at"}, {PID: "1", Region: "de"}}
	started := make(chan taskconfig.Task, len(tasks))
	release := make(chan struct{})
	var once sync.Once
	releaseAll := func() { once.Do(func() { close(release) }) }
	defer releaseAll()
	wantErr := errors.New("one worker failed")
	done := make(chan error, 1)
	go func() {
		done <- runTaskWorkers(context.Background(), tasks, func(_ context.Context, task taskconfig.Task, index int) error {
			started <- task
			<-release
			if index == 0 {
				return wantErr
			}
			return nil
		})
	}()
	seen := map[taskconfig.Task]bool{}
	for range tasks {
		select {
		case task := <-started:
			seen[task] = true
		case <-time.After(2 * time.Second):
			t.Fatal("workers did not start concurrently")
		}
	}
	releaseAll()
	if err := <-done; !errors.Is(err, wantErr) || len(seen) != len(tasks) {
		t.Fatalf("err=%v, seen=%v", err, seen)
	}
}

type notifierFunc func(context.Context, Event) error

func (f notifierFunc) Notify(ctx context.Context, event Event) error { return f(ctx, event) }

func TestTaskNotifiesOnceAndRetriesWithoutRefetching(t *testing.T) {
	var events []Event
	fetches, notifications := 0, 0
	task := taskconfig.Task{PID: "2087300", Region: "at"}
	err := watchTask(context.Background(), task, time.Second,
		func(context.Context) (json.RawMessage, error) {
			fetches++
			if fetches == 1 {
				return availabilityJSON("OutOfStock"), nil
			}
			return availabilityJSON("InStock"), nil
		}, notifierFunc(func(_ context.Context, event Event) error {
			notifications++
			if event.PID != task.PID || event.Country != task.Region {
				t.Fatalf("wrong notification task: %+v", event)
			}
			if notifications == 1 {
				return &NotificationError{Message: "temporary failure", Retryable: true}
			}
			return nil
		}), func(event Event) error {
			events = append(events, event)
			return nil
		}, func(context.Context, time.Duration) error { return nil })
	if err != nil || fetches != 2 || notifications != 2 || len(events) != 4 {
		t.Fatalf("err=%v, fetches=%d, notifications=%d, events=%v", err, fetches, notifications, events)
	}
	for i, want := range []string{"checked", "in_stock", "notification_retry", "notified"} {
		if events[i].Event != want || events[i].PID != task.PID || events[i].Country != task.Region {
			t.Fatalf("wrong event %d: %+v", i, events[i])
		}
	}
}

func TestTaskReportsPermanentNotificationFailure(t *testing.T) {
	var events []Event
	failure := &NotificationError{Message: "webhook deleted"}
	err := watchTask(context.Background(), taskconfig.Task{PID: "2087300", Region: "at"}, time.Second,
		func(context.Context) (json.RawMessage, error) { return availabilityJSON("InStock"), nil },
		notifierFunc(func(context.Context, Event) error { return failure }),
		func(event Event) error { events = append(events, event); return nil },
		func(context.Context, time.Duration) error { t.Fatal("unexpected retry"); return nil })
	if !errors.Is(err, failure) || len(events) != 2 || events[1].Event != "notification_error" || !json.Valid(events[1].Product) {
		t.Fatalf("err=%v, events=%v", err, events)
	}
}

func TestErrorStreakAlertsOnceRecoversAndCanAlertAgain(t *testing.T) {
	calls := 0
	var notifications []string
	var events []Event
	err := watchTaskWithAlerts(context.Background(), taskconfig.Task{PID: "2087300", Region: "at"}, time.Second,
		func(context.Context) (json.RawMessage, error) {
			calls++
			if calls == 5 {
				return availabilityJSON("OutOfStock"), nil
			}
			if calls == 8 {
				return availabilityJSON("InStock"), nil
			}
			return nil, errors.New("proxy unavailable")
		}, notifierFunc(func(_ context.Context, event Event) error {
			notifications = append(notifications, event.Event)
			return nil
		}),
		func(event Event) error { events = append(events, event); return nil }, func(context.Context, time.Duration) error { return nil }, 2)
	want := []string{"monitor_warning", "monitor_recovered", "monitor_warning", "monitor_recovered", "in_stock"}
	if err != nil || calls != 8 || fmt.Sprint(notifications) != fmt.Sprint(want) {
		t.Fatalf("err=%v, calls=%d, notifications=%v", err, calls, notifications)
	}
	for _, event := range events {
		if event.PID != "2087300" || event.Country != "at" {
			t.Fatalf("alert lost task identity: %+v", event)
		}
	}
}

func TestBrokenHealthWebhookDoesNotStopChecks(t *testing.T) {
	calls, notifications, alertErrors := 0, 0, 0
	err := watchTaskWithAlerts(context.Background(), taskconfig.Task{PID: "1", Region: "de"}, time.Second,
		func(context.Context) (json.RawMessage, error) {
			calls++
			if calls < 4 {
				return nil, errors.New("HTTP 403")
			}
			return availabilityJSON("InStock"), nil
		}, notifierFunc(func(_ context.Context, event Event) error {
			notifications++
			if event.Event != "in_stock" {
				return &NotificationError{Message: "webhook rejected"}
			}
			return nil
		}), func(event Event) error {
			if event.Event == "alert_error" {
				alertErrors++
			}
			return nil
		},
		func(context.Context, time.Duration) error { return nil }, 2)
	if err != nil || calls != 4 || notifications != 3 || alertErrors != 2 {
		t.Fatalf("err=%v, calls=%d, notifications=%d, alertErrors=%d", err, calls, notifications, alertErrors)
	}
}

func TestHealthDeliveryRetriesAndOutputFailureIsPreserved(t *testing.T) {
	calls := 0
	var delays []time.Duration
	notifier := notifierFunc(func(ctx context.Context, _ Event) error {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 15*time.Second {
			t.Fatal("health delivery must be bounded")
		}
		calls++
		if calls == 1 {
			return &NotificationError{Message: "rate limited", Retryable: true}
		}
		return nil
	})
	err := sendHealthAlert(context.Background(), Event{Event: "monitor_warning"}, notifier, func(Event) error { return nil },
		func(_ context.Context, delay time.Duration) error { delays = append(delays, delay); return nil })
	if err != nil || calls != 2 || len(delays) != 1 {
		t.Fatalf("calls=%d, delays=%v, err=%v", calls, delays, err)
	}
	want := errors.New("closed log output")
	err = sendHealthAlert(context.Background(), Event{Event: "monitor_warning"}, notifier, func(Event) error { return want }, func(context.Context, time.Duration) error { return nil })
	if !errors.Is(err, want) || calls != 2 {
		t.Fatalf("output failure ignored: %v", err)
	}
}
