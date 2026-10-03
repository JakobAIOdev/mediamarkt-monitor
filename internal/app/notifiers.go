package app

import (
	"fmt"
	"strings"

	"mediamarkt-monitor/internal/checker"
	"mediamarkt-monitor/internal/discord"
	"mediamarkt-monitor/internal/tasks"
)

type taskKey struct{ pid, region string }

// taskNotifiers is built before workers start and only read during monitoring.
// Tasks using the same normalized URL share one client and rate-limit gate.
type taskNotifiers struct {
	routes  map[taskKey]checker.Notifier
	clients map[string]*discord.Notifier
}

func newTaskNotifiers(taskList []tasks.Task, defaultWebhook string) (*taskNotifiers, error) {
	n := &taskNotifiers{
		routes:  make(map[taskKey]checker.Notifier),
		clients: make(map[string]*discord.Notifier),
	}
	for _, task := range taskList {
		webhook := strings.TrimSpace(task.WebhookURL)
		if webhook == "" {
			webhook = strings.TrimSpace(defaultWebhook)
		}
		if webhook == "" {
			continue
		}
		normalized, err := discord.NormalizeWebhookURL(webhook)
		if err != nil {
			n.Close()
			return nil, fmt.Errorf("task %s/%s: %w", task.Region, task.PID, err)
		}
		client := n.clients[normalized]
		if client == nil {
			client, err = discord.New(normalized)
			if err != nil {
				n.Close()
				return nil, fmt.Errorf("task %s/%s: %w", task.Region, task.PID, err)
			}
			n.clients[normalized] = client
		}
		n.routes[taskKey{task.PID, task.Region}] = client
	}
	return n, nil
}

func (n *taskNotifiers) ForTask(task tasks.Task) checker.Notifier {
	return n.routes[taskKey{task.PID, task.Region}]
}

func (n *taskNotifiers) Close() {
	for _, client := range n.clients {
		client.Close()
	}
}
