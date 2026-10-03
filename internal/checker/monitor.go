package checker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"mediamarkt-monitor/internal/retry"
	taskconfig "mediamarkt-monitor/internal/tasks"
)

// Notifier delivers an in_stock event. Temporary failures should return a
// NotificationError with Retryable set so the checker can retry delivery.
type Notifier interface {
	Notify(context.Context, Event) error
}

// NotifierResolver selects a task's notification destination. A nil result
// disables notifications for that task. Resolution must be safe across workers.
type NotifierResolver func(taskconfig.Task) Notifier

// Run fetches each task once or watches it until stock and optional notification
// delivery are confirmed. Each task owns its client; output is serialized.
func Run(ctx context.Context, tasks []taskconfig.Task, proxies []string, watch bool, interval time.Duration, notifierForTask NotifierResolver, output io.Writer) error {
	if interval <= 0 {
		return fmt.Errorf("interval must be greater than zero")
	}
	if notifierForTask != nil && !watch {
		return fmt.Errorf("Discord notifications require -watch")
	}
	encoder := json.NewEncoder(output)
	var outputMu sync.Mutex
	writeEvent := func(event Event) error {
		outputMu.Lock()
		defer outputMu.Unlock()
		return encoder.Encode(event)
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
			outputMu.Lock()
			defer outputMu.Unlock()
			if len(tasks) == 1 {
				encoder.SetIndent("", "  ")
				return encoder.Encode(product)
			}
			return encoder.Encode(Event{Time: time.Now().UTC(), PID: task.PID, Country: task.Region, Event: "product", Product: product})
		}
		var notifier Notifier
		if notifierForTask != nil {
			notifier = notifierForTask(task)
		}
		return watchTask(ctx, task, interval, fetcher.Fetch, notifier, writeEvent, retry.Wait)
	})
}

func watchTask(ctx context.Context, task taskconfig.Task, interval time.Duration, fetch func(context.Context) (json.RawMessage, error), notifier Notifier, emit func(Event) error, wait func(context.Context, time.Duration) error) error {
	return checkUntilInStock(ctx, interval, fetch, func(event Event) error {
		event.PID, event.Country = task.PID, task.Region
		if err := emit(event); err != nil {
			return err
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
