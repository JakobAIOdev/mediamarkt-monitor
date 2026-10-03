package checker

import (
	"context"
	"errors"
	"time"
)

// NotificationError tells the checker whether delivery can be retried and
// supplies any minimum delay required by the notification service.
type NotificationError struct {
	Message    string
	Retryable  bool
	RetryAfter time.Duration
}

func (e *NotificationError) Error() string { return e.Message }

// NotifyUntilSent retries temporary delivery failures without fetching the
// product again. Permanent failures and context cancellation stop delivery.
func NotifyUntilSent(ctx context.Context, event Event, notifier Notifier, emit func(Event) error, wait func(context.Context, time.Duration) error) error {
	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := notifier.Notify(ctx, event)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var failure *NotificationError
		if !errors.As(err, &failure) || !failure.Retryable {
			return err
		}
		delay := notificationRetryDelay(attempt, failure.RetryAfter)
		if err := emit(Event{Time: time.Now().UTC(), PID: event.PID, Country: event.Country, Event: "notification_retry", NotificationFor: event.Event, Attempt: attempt, Error: err.Error(), RetryIn: delay.String()}); err != nil {
			return err
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
}

func notificationRetryDelay(attempt int, retryAfter time.Duration) time.Duration {
	delay := 5 * time.Second
	for i := 1; i < attempt && delay < time.Minute; i++ {
		delay = min(delay*2, time.Minute)
	}
	return max(delay, retryAfter)
}
