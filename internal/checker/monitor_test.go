package checker

import (
	"context"
	"encoding/json"
	"errors"
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
