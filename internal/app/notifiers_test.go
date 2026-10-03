package app

import (
	"strings"
	"testing"

	"mediamarkt-monitor/internal/tasks"
)

func TestWebhookOverridesAndEnvironmentFallbackShareClients(t *testing.T) {
	defaultURL := "https://discord.com/api/webhooks/123/default-token"
	overrideURL := "https://discord.com/api/webhooks/456/task-token"
	taskList := []tasks.Task{
		{PID: "1", Region: "at"},
		{PID: "1", Region: "de", WebhookURL: overrideURL},
		{PID: "2", Region: "at", WebhookURL: defaultURL + "?wait=false"},
	}
	n, err := newTaskNotifiers(taskList, " "+defaultURL+" ")
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	fallback, override, shared := n.ForTask(taskList[0]), n.ForTask(taskList[1]), n.ForTask(taskList[2])
	if fallback == nil || override == nil || fallback == override || fallback != shared || len(n.clients) != 2 {
		t.Fatal("task routing did not override, fall back or reuse the shared client")
	}
}

func TestBlankDefaultDisablesOnlyTasksWithoutOverrides(t *testing.T) {
	taskList := []tasks.Task{
		{PID: "1", Region: "at"},
		{PID: "2", Region: "at", WebhookURL: "https://discord.com/api/webhooks/123/task-token"},
	}
	n, err := newTaskNotifiers(taskList, " ")
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if n.ForTask(taskList[0]) != nil || n.ForTask(taskList[1]) == nil {
		t.Fatal("a task without a destination must have no notifier")
	}
}

func TestWebhookValidationUsesOnlyEffectiveDestinations(t *testing.T) {
	task := tasks.Task{PID: "2087300", Region: "at", WebhookURL: "https://discord.com/api/webhooks/123/task-token"}
	n, err := newTaskNotifiers([]tasks.Task{task}, "unused-invalid-default")
	if err != nil {
		t.Fatal("an overridden default should not be used or validated")
	}
	n.Close()
	task.WebhookURL = "https://user:secret@discord.com/api/webhooks/123/task-token"
	_, err = newTaskNotifiers([]tasks.Task{task}, "")
	if err == nil || !strings.Contains(err.Error(), "at/2087300") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "task-token") {
		t.Fatal("invalid task URLs must identify the task without exposing credentials")
	}
}
