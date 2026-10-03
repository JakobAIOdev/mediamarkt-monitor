// Package app parses CLI configuration and wires the monitor's dependencies.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"mediamarkt-monitor/internal/checker"
	"mediamarkt-monitor/internal/proxy"
	"mediamarkt-monitor/internal/tasks"
)

type options struct {
	country     string
	watch       bool
	interval    time.Duration
	tasksFile   string
	proxiesFile string
}

// Run accepts command-line arguments without the program name. Environment
// comes from the caller so configuration and output do not rely on global state.
func Run(ctx context.Context, args []string, output, diagnostics io.Writer, env Environment) error {
	opts, pids, err := parseOptions(args, diagnostics, env)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	return run(ctx, opts, pids, output, env.DiscordWebhookURL)
}

func parseOptions(args []string, diagnostics io.Writer, env Environment) (options, []string, error) {
	var opts options
	flags := flag.NewFlagSet("mediamarkt-monitor", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&opts.country, "country", "at", "MediaMarkt country: at or de")
	flags.BoolVar(&opts.watch, "watch", false, "check repeatedly until the product is InStock")
	flags.DurationVar(&opts.interval, "interval", 30*time.Second, "delay between successful checks; overrides CHECK_INTERVAL")
	flags.StringVar(&opts.tasksFile, "tasks", "", "CSV file with pid,region and optional webhook_url columns")
	flags.StringVar(&opts.proxiesFile, "proxies", "", "proxy list, one proxy per line")
	if err := flags.Parse(args); err != nil {
		return opts, nil, err
	}
	intervalSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "interval" {
			intervalSet = true
		}
	})
	if value := strings.TrimSpace(env.CheckInterval); !intervalSet && value != "" {
		interval, err := time.ParseDuration(value)
		if err != nil || interval <= 0 {
			return opts, nil, fmt.Errorf("CHECK_INTERVAL must be a positive duration such as 30s or 1m")
		}
		opts.interval = interval
	}
	return opts, flags.Args(), nil
}

func run(ctx context.Context, opts options, pids []string, output io.Writer, webhook string) error {
	taskList, err := tasks.Load(opts.tasksFile, pids, opts.country)
	if err != nil {
		return err
	}
	if opts.interval <= 0 {
		return fmt.Errorf("interval must be greater than zero")
	}
	proxyList, err := proxy.Load(opts.proxiesFile)
	if err != nil {
		return err
	}
	var notifierForTask checker.NotifierResolver
	if opts.watch {
		notifiers, err := newTaskNotifiers(taskList, webhook)
		if err != nil {
			return err
		}
		defer notifiers.Close()
		notifierForTask = notifiers.ForTask
	}
	return checker.Run(ctx, taskList, proxyList, opts.watch, opts.interval, notifierForTask, output)
}
