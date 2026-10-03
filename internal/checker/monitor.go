package checker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"mediamarkt-monitor/internal/retry"
	taskconfig "mediamarkt-monitor/internal/tasks"
)

// Notifier delivers stock and monitor health events. Temporary failures return a
// NotificationError with Retryable set so the checker can retry delivery.
type Notifier interface {
	Notify(context.Context, Event) error
}

// NotifierResolver selects a task's notification destination. A nil result
// disables notifications for that task. Resolution must be safe across workers.
type NotifierResolver func(taskconfig.Task) Notifier

// Settings selects monitoring behavior and output without coupling workers to
// a terminal or JSON encoder. Callbacks are serialized across workers.
type Settings struct {
	Watch           bool
	Interval        time.Duration
	AlertAfter      int
	NotifierForTask NotifierResolver
	Emit            func(Event) error
	Snapshot        func(json.RawMessage) error
}

// Run fetches each task once or watches it until stock and optional notification
// delivery are confirmed. Each task owns its client; output is serialized.
func Run(ctx context.Context, tasks []taskconfig.Task, proxies []string, settings Settings) error {
	watch, interval, notifierForTask := settings.Watch, settings.Interval, settings.NotifierForTask
	if interval <= 0 {
		return fmt.Errorf("interval must be greater than zero")
	}
	if notifierForTask != nil && !watch {
		return fmt.Errorf("Discord notifications require -watch")
	}
	var outputMu sync.Mutex
	writeEvent := func(event Event) error {
		outputMu.Lock()
		defer outputMu.Unlock()
		return settings.Emit(event)
	}
	return runTaskWorkers(ctx, tasks, func(ctx context.Context, task taskconfig.Task, worker int) error {
		fetcher := newProductFetcher(task, proxies, worker)
		defer fetcher.Close()
		if !watch {
			product, err := fetcher.Fetch(ctx)
			if err != nil {
				if outputErr := writeEvent(Event{Time: time.Now().UTC(), PID: task.PID, Country: task.Region, Event: "error", Error: err.Error()}); outputErr != nil {
					return errors.Join(err, outputErr)
				}
				return err
			}
			if len(tasks) == 1 {
				outputMu.Lock()
				defer outputMu.Unlock()
				return settings.Snapshot(product)
			}
			return writeEvent(Event{Time: time.Now().UTC(), PID: task.PID, Country: task.Region, Event: "product", Product: product})
		}
		var notifier Notifier
		if notifierForTask != nil {
			notifier = notifierForTask(task)
		}
		return watchTaskWithAlerts(ctx, task, interval, fetcher.Fetch, notifier, writeEvent, retry.Wait, settings.AlertAfter)
	})
}

func watchTask(ctx context.Context, task taskconfig.Task, interval time.Duration, fetch func(context.Context) (json.RawMessage, error), notifier Notifier, emit func(Event) error, wait func(context.Context, time.Duration) error) error {
	return watchTaskWithAlerts(ctx, task, interval, fetch, notifier, emit, wait, 0)
}

func watchTaskWithAlerts(ctx context.Context, task taskconfig.Task, interval time.Duration, fetch func(context.Context) (json.RawMessage, error), notifier Notifier, emit func(Event) error, wait func(context.Context, time.Duration) error, alertAfter int) error {
	failures, warned := 0, false
	return checkUntilInStock(ctx, interval, fetch, func(event Event) error {
		event.PID, event.Country = task.PID, task.Region
		if err := emit(event); err != nil {
			return err
		}
		if event.Event == "error" {
			failures++
			if alertAfter > 0 && failures >= alertAfter && !warned {
				warned = true
				alert := Event{Time: event.Time, PID: task.PID, Country: task.Region, Event: "monitor_warning", Error: event.Error, RetryIn: event.RetryIn, ConsecutiveErrors: failures}
				return sendHealthAlert(ctx, alert, notifier, emit, wait)
			}
		} else {
			if warned {
				alert := Event{Time: event.Time, PID: task.PID, Country: task.Region, Event: "monitor_recovered", Availability: event.Availability, ConsecutiveErrors: failures}
				if err := sendHealthAlert(ctx, alert, notifier, emit, wait); err != nil {
					return err
				}
			}
			failures, warned = 0, false
		}
		if event.Event != "in_stock" || notifier == nil {
			return nil
		}
		if err := NotifyUntilSent(ctx, event, notifier, emit, wait); err != nil {
			if ctx.Err() == nil {
				if outputErr := emit(Event{Time: time.Now().UTC(), PID: task.PID, Country: task.Region, Event: "notification_error", Error: err.Error(), Product: event.Product}); outputErr != nil {
					return errors.Join(err, outputErr)
				}
			}
			return err
		}
		return emit(Event{Time: time.Now().UTC(), PID: task.PID, Country: task.Region, Event: "notified"})
	}, wait)
}

// Health alerts are best effort and bounded. A broken webhook must never stop
// product checks or wait indefinitely behind another task's notification.
func sendHealthAlert(ctx context.Context, event Event, notifier Notifier, emit func(Event) error, wait func(context.Context, time.Duration) error) error {
	if err := emit(event); err != nil {
		return err
	}
	if notifier == nil {
		return nil
	}
	alertCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var outputErr error
	err := NotifyUntilSent(alertCtx, event, notifier, func(retryEvent Event) error {
		outputErr = emit(retryEvent)
		return outputErr
	}, wait)
	if outputErr != nil {
		return outputErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	result := Event{Time: time.Now().UTC(), PID: event.PID, Country: event.Country, Event: "alert_sent", NotificationFor: event.Event}
	if err != nil {
		result.Event, result.Error = "alert_error", err.Error()
	}
	return emit(result)
}

func runTaskWorkers(ctx context.Context, tasks []taskconfig.Task, run func(context.Context, taskconfig.Task, int) error) error {
	var workers sync.WaitGroup
	results := make(chan error, len(tasks))
	for index, task := range tasks {
		workers.Add(1)
		go func(task taskconfig.Task, worker int) {
			defer workers.Done()
			if err := run(ctx, task, worker); err != nil {
				results <- fmt.Errorf("%s/%s: %w", task.Region, task.PID, err)
			}
		}(task, index)
	}
	workers.Wait()
	close(results)
	var failures []error
	for err := range results {
		if !errors.Is(err, context.Canceled) {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return ctx.Err()
}
