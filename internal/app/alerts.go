package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"mediamarkt-monitor/internal/checker"
	"mediamarkt-monitor/internal/discord"
	"mediamarkt-monitor/internal/retry"
)

// Process-wide failures use the default webhook because task configuration may
// itself be unreadable. Failure reporting cannot replace the original error.
func reportFailure(ctx context.Context, webhook string, failure error, diagnostics io.Writer) {
	if strings.TrimSpace(webhook) == "" {
		return
	}
	notifier, err := discord.New(webhook)
	if err != nil {
		return
	}
	defer notifier.Close()
	if err := deliverFailure(ctx, notifier, failure); err != nil {
		fmt.Fprintln(diagnostics, "Discord failure alert could not be delivered:", cleanLine(err.Error(), 180))
	} else {
		fmt.Fprintln(diagnostics, "Discord failure alert delivered.")
	}
}

func deliverFailure(ctx context.Context, notifier checker.Notifier, failure error) error {
	alertCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	event := checker.Event{Time: time.Now().UTC(), Event: "monitor_failed", Error: failure.Error()}
	return checker.NotifyUntilSent(alertCtx, event, notifier, func(checker.Event) error { return nil }, retry.Wait)
}
