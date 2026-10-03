// Package app parses CLI configuration and wires the monitor's dependencies.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"mediamarkt-monitor/internal/checker"
	"mediamarkt-monitor/internal/proxy"
	"mediamarkt-monitor/internal/tasks"
)

type options struct {
	country      string
	watch        bool
	interval     time.Duration
	tasksFile    string
	proxiesFile  string
	outputFormat string
	validate     bool
	alertAfter   int
}

// Run accepts command-line arguments without the program name. Environment
// comes from the caller so configuration and output do not rely on global state.
func Run(ctx context.Context, args []string, output, diagnostics io.Writer, env Environment) (result error) {
	opts, pids, err := parseOptions(args, diagnostics, env)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	defer func() {
		if result != nil && opts.watch && ctx.Err() == nil {
			reportFailure(ctx, env.DiscordWebhookURL, result, diagnostics)
		}
	}()
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
	flags.StringVar(&opts.outputFormat, "output", "auto", "output format: auto, pretty or json (auto: pretty in watch mode)")
	flags.BoolVar(&opts.validate, "validate", false, "validate configuration without product requests or stock notifications")
	if err := flags.Parse(args); err != nil {
		return opts, nil, err
	}
	if opts.outputFormat != "auto" && opts.outputFormat != "pretty" && opts.outputFormat != "json" {
		return opts, nil, fmt.Errorf("output must be auto, pretty or json")
	}
	opts.alertAfter = 5
	if value := strings.TrimSpace(env.ErrorAlertThreshold); value != "" {
		threshold, err := strconv.Atoi(value)
		if err != nil || threshold < 1 {
			return opts, nil, fmt.Errorf("ERROR_ALERT_THRESHOLD must be a positive integer")
		}
		opts.alertAfter = threshold
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
	if err := ctx.Err(); err != nil {
		return err
	}
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
	reporter := newReporter(output, opts.outputFormat, opts.watch)
	if opts.validate {
		return reporter.Validated()
	}
	if opts.watch {
		if err := reporter.Start(taskList, len(proxyList), opts.interval, opts.alertAfter, notifierForTask); err != nil {
			return err
		}
	}
	err = checker.Run(ctx, taskList, proxyList, checker.Settings{
		Watch: opts.watch, Interval: opts.interval, AlertAfter: opts.alertAfter,
		NotifierForTask: notifierForTask, Emit: reporter.Event, Snapshot: reporter.Product,
	})
	if opts.watch {
		if outputErr := reporter.Finish(err); outputErr != nil {
			return errors.Join(err, outputErr)
		}
	}
	return err
}
