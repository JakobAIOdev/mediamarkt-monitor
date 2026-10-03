// Package tasks loads and deduplicates PID/region tasks from CSV and CLI input.
package tasks

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"mediamarkt-monitor/internal/product"
)

type Task struct {
	PID        string
	Region     string
	WebhookURL string `json:"-"`
}

// Load combines CSV rows and positional PIDs. Country applies only to positional
// PIDs; every resulting task has a validated PID and normalized region.
func Load(path string, positional []string, country string) ([]Task, error) {
	var tasks []Task
	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open tasks CSV: %w", err)
		}
		defer file.Close()
		tasks, err = readTasksCSV(file)
		if err != nil {
			return nil, err
		}
	}
	for _, pid := range positional {
		region, pid, err := product.ValidateInput(country, pid)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, Task{PID: pid, Region: region})
	}
	tasks, err := uniqueTasks(tasks)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("provide at least one PID or a tasks CSV with -tasks")
	}
	return tasks, nil
}

func readTasksCSV(input io.Reader) ([]Task, error) {
	r := csv.NewReader(input)
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read tasks CSV header (expected pid,region): %w", err)
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "\ufeff")))
		if _, exists := columns[name]; exists {
			return nil, fmt.Errorf("duplicate tasks CSV column %q", name)
		}
		columns[name] = i
	}
	pidColumn, hasPID := columns["pid"]
	regionColumn, hasRegion := columns["region"]
	webhookColumn, hasWebhook := columns["webhook_url"]
	if !hasPID || !hasRegion {
		return nil, fmt.Errorf("tasks CSV requires columns pid and region")
	}
	var tasks []Task
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tasks CSV: %w", err)
		}
		line, _ := r.FieldPos(pidColumn)
		region, pid, err := product.ValidateInput(row[regionColumn], row[pidColumn])
		if err != nil {
			return nil, fmt.Errorf("tasks CSV line %d: %w", line, err)
		}
		task := Task{PID: pid, Region: region}
		if hasWebhook {
			task.WebhookURL = strings.TrimSpace(row[webhookColumn])
		}
		tasks = append(tasks, task)
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("tasks CSV contains no tasks")
	}
	return uniqueTasks(tasks)
}

func uniqueTasks(tasks []Task) ([]Task, error) {
	type key struct{ pid, region string }
	seen := make(map[key]int)
	result := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		identity := key{task.PID, task.Region}
		if index, exists := seen[identity]; exists {
			previous := result[index].WebhookURL
			if previous != "" && task.WebhookURL != "" && previous != task.WebhookURL {
				return nil, fmt.Errorf("conflicting webhook_url values for task %s/%s", task.Region, task.PID)
			}
			if previous == "" {
				result[index].WebhookURL = task.WebhookURL
			}
			continue
		}
		seen[identity] = len(result)
		result = append(result, task)
	}
	return result, nil
}
