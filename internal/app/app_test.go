package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpDoesNotRequireTasks(t *testing.T) {
	var output, diagnostics bytes.Buffer
	err := Run(context.Background(), []string{"-h"}, &output, &diagnostics, Environment{CheckInterval: "invalid"})
	if err != nil || output.Len() != 0 || !strings.Contains(diagnostics.String(), "-tasks") || !strings.Contains(diagnostics.String(), "-proxies") {
		t.Fatalf("err=%v, output=%q, diagnostics=%q", err, output.String(), diagnostics.String())
	}
}

func TestInvalidConfigurationFailsBeforeFetching(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		webhook string
		want    string
	}{
		{[]string{"-watch", "-interval", "0s", "2087300"}, "", "interval must be greater than zero"},
		{[]string{"-watch", "2087300"}, "invalid-webhook", "Discord webhook URL"},
	} {
		var output bytes.Buffer
		err := Run(context.Background(), tc.args, &output, io.Discard, Environment{DiscordWebhookURL: tc.webhook})
		if err == nil || !strings.Contains(err.Error(), tc.want) || output.Len() != 0 {
			t.Fatalf("args=%v, err=%v, output=%q", tc.args, err, output.String())
		}
	}
}

func TestCSVAndProxyConfigurationHonorsCancellation(t *testing.T) {
	directory := t.TempDir()
	tasksFile := filepath.Join(directory, "tasks.csv")
	proxiesFile := filepath.Join(directory, "proxies.txt")
	if err := os.WriteFile(tasksFile, []byte("pid,region,webhook_url\n2087300,AT,https://discord.com/api/webhooks/123/task-token\n2087300,DE,\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proxiesFile, []byte("proxy.example:8080:user:password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	err := Run(ctx, []string{"-watch", "-tasks", tasksFile, "-proxies", proxiesFile, "2087300"}, &output, io.Discard, Environment{DiscordWebhookURL: "https://discord.com/api/webhooks/456/default-token"})
	if !errors.Is(err, context.Canceled) || output.Len() != 0 {
		t.Fatalf("err=%v, output=%q", err, output.String())
	}
}
