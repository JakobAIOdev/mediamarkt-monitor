package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"mediamarkt-monitor/internal/checker"
	"mediamarkt-monitor/internal/tasks"
)

// The pretty view uses complete lines, so terminal sessions and service logs
// stay readable without cursor controls or terminal escape sequences.
type reporter struct {
	output io.Writer
	json   *json.Encoder
	pretty bool
}

func newReporter(output io.Writer, format string, watch bool) *reporter {
	return &reporter{output: output, json: json.NewEncoder(output), pretty: format == "pretty" || (format == "auto" && watch)}
}

func (r *reporter) Start(taskList []tasks.Task, proxies int, interval time.Duration, alertAfter int, resolve checker.NotifierResolver) error {
	if !r.pretty {
		return nil
	}
	notifications := 0
	for _, task := range taskList {
		if resolve != nil && resolve(task) != nil {
			notifications++
		}
	}
	connection := fmt.Sprintf("%d proxies", proxies)
	if proxies == 0 {
		connection = "direct connection"
	}
	_, err := fmt.Fprintf(r.output, "\n  MEDIAMARKT MONITOR\n  %s\n  Tasks    %d  |  Interval %s  |  %s\n  Discord  %d/%d tasks  |  Error alert after %d failures\n  %s\n", strings.Repeat("─", 66), len(taskList), interval, connection, notifications, len(taskList), alertAfter, strings.Repeat("─", 66))
	return err
}

func (r *reporter) Validated() error {
	if !r.pretty {
		return r.json.Encode(map[string]string{"event": "validated"})
	}
	_, err := fmt.Fprintln(r.output, "Configuration valid. Tasks, proxies and webhook URLs checked.")
	return err
}

func (r *reporter) Event(event checker.Event) error {
	if !r.pretty {
		return r.json.Encode(event)
	}
	label, detail := event.Event, ""
	switch event.Event {
	case "checked":
		label, detail = "WAITING", displayAvailability(event.Availability)
	case "in_stock":
		label, detail = "IN STOCK", productLabel(event.Product)
	case "error":
		label, detail = "ERROR", event.Error
	case "notified":
		label, detail = "DISCORD", "Stock notification delivered"
	case "notification_retry":
		label, detail = "RETRY", "Discord: "+event.Error
	case "notification_error":
		label, detail = "FAILED", "Stock notification: "+event.Error
	case "monitor_warning":
		label, detail = "DEGRADED", fmt.Sprintf("%d consecutive errors; checks continue", event.ConsecutiveErrors)
	case "monitor_recovered":
		label, detail = "RECOVERED", "Product checks working again"
	case "alert_sent":
		label, detail = "DISCORD", "Monitor alert delivered"
	case "alert_error":
		label, detail = "ALERT FAILED", event.Error+"; checks continue"
	case "product":
		label, detail = "PRODUCT", productLabel(event.Product)
	}
	if event.Attempt > 0 {
		detail += fmt.Sprintf(" | #%d", event.Attempt)
	}
	if event.RetryIn != "" {
		detail += " | next " + event.RetryIn
	}
	_, err := fmt.Fprintf(r.output, "  %s  %-2s  %-9s  %-12s  %s\n", event.Time.Local().Format("15:04:05"), strings.ToUpper(event.Country), event.PID, label, cleanLine(detail, 180))
	return err
}

func (r *reporter) Product(product json.RawMessage) error {
	if r.pretty {
		_, err := fmt.Fprintln(r.output, cleanLine(productLabel(product), 180))
		return err
	}
	encoder := json.NewEncoder(r.output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(product)
}

func (r *reporter) Finish(result error) error {
	if !r.pretty {
		return nil
	}
	message := "All tasks completed."
	if errors.Is(result, context.Canceled) {
		message = "Monitor stopped."
	} else if result != nil {
		message = "Monitor finished with errors. Check the details above."
	}
	_, err := fmt.Fprintf(r.output, "  %s\n  %s\n\n", strings.Repeat("─", 66), message)
	return err
}

func productLabel(raw json.RawMessage) string {
	var product struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &product) == nil && product.Name != "" {
		return product.Name
	}
	return "Product available"
}

func displayAvailability(status string) string {
	replacer := strings.NewReplacer("OutOfStock", "Out of stock", "PreOrder", "Pre-order", "BackOrder", "Back-order", ",", ", ")
	return replacer.Replace(status)
}

// Remote names and error messages cannot inject new lines or terminal controls.
func cleanLine(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return string(runes)
}
