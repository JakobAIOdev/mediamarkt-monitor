package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDotEnvLoadingAndProcessOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# Config\nCHECK_INTERVAL=\"45s\" # delay\nERROR_ALERT_THRESHOLD=7\nexport DISCORD_WEBHOOK_URL='https://discord.com/api/webhooks/123/test-token'\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	noVariables := func(string) (string, bool) { return "", false }
	env, err := LoadEnvironment(path, noVariables)
	if err != nil || env.CheckInterval != "45s" || env.ErrorAlertThreshold != "7" || env.DiscordWebhookURL != "https://discord.com/api/webhooks/123/test-token" {
		t.Fatal("quoted .env configuration was not loaded")
	}
	env, err = LoadEnvironment(path, func(key string) (string, bool) {
		if key == "CHECK_INTERVAL" {
			return "2m", true
		}
		if key == "DISCORD_WEBHOOK_URL" {
			return "", true // An explicitly empty process variable disables the default.
		}
		return "", false
	})
	if err != nil || env.CheckInterval != "2m" || env.DiscordWebhookURL != "" {
		t.Fatal("process configuration did not take precedence")
	}
	env, err = LoadEnvironment(filepath.Join(t.TempDir(), ".env"), noVariables)
	if err != nil || env != (Environment{}) {
		t.Fatal("missing optional .env should use defaults")
	}
}

func TestOutputAndAlertThresholdValidation(t *testing.T) {
	for _, value := range []string{"0", "-1", "private-token", "1.5"} {
		_, _, err := parseOptions(nil, io.Discard, Environment{ErrorAlertThreshold: value})
		if err == nil || !strings.Contains(err.Error(), "ERROR_ALERT_THRESHOLD") || strings.Contains(err.Error(), value) {
			t.Fatalf("unsafe threshold error: %v", err)
		}
	}
	opts, _, err := parseOptions([]string{"-watch", "-output", "json"}, io.Discard, Environment{ErrorAlertThreshold: "7"})
	if err != nil || opts.alertAfter != 7 || opts.outputFormat != "json" {
		t.Fatalf("opts=%+v, err=%v", opts, err)
	}
	if _, _, err := parseOptions([]string{"-output", "invalid"}, io.Discard, Environment{}); err == nil {
		t.Fatal("invalid output mode accepted")
	}
}

func TestMalformedDotEnvDoesNotExposeSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("SECRET?private-token=value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadEnvironment(path, func(string) (string, bool) { return "", false })
	if err == nil || !strings.Contains(err.Error(), "invalid .env") || strings.Contains(err.Error(), "private-token") {
		t.Fatal("malformed dotenv configuration must fail without exposing its source")
	}
}

func TestCheckIntervalPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		args []string
		want time.Duration
	}{
		{"default", "", nil, 30 * time.Second},
		{"dotenv", " 1m ", nil, time.Minute},
		{"flag overrides env", "2m", []string{"-interval", "45s"}, 45 * time.Second},
		{"flag overrides invalid env", "invalid", []string{"-interval", "15s"}, 15 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, _, err := parseOptions(tc.args, io.Discard, Environment{CheckInterval: tc.env})
			if err != nil || opts.interval != tc.want {
				t.Fatalf("interval=%v, want=%v, err=%v", opts.interval, tc.want, err)
			}
		})
	}
	for _, value := range []string{"0s", "-1s", "30", "bad-secret"} {
		_, _, err := parseOptions(nil, io.Discard, Environment{CheckInterval: value})
		if err == nil || !strings.Contains(err.Error(), "CHECK_INTERVAL") || strings.Contains(err.Error(), "bad-secret") {
			t.Fatal("invalid CHECK_INTERVAL must fail without echoing its value")
		}
	}
}
