package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"artifactdownloader/internal/application"
	"artifactdownloader/internal/config"
	"artifactdownloader/internal/environmentconfig"
)

// main 以實際程序參數及標準輸出入呼叫 run，並將其輸出設為程序結束碼。
// 輸入來自 os.Args；輸出為程序 exit code，不直接回傳值。
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run 解析 validate/run CLI、載入設定、執行工作並輸出摘要。
// 輸入為不含程式名的 args 與 stdout/stderr writer；輸出為 0、1、2 或 130 結束碼。
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "validate":
		flags := flag.NewFlagSet("validate", flag.ContinueOnError)
		flags.SetOutput(stderr)
		configPath := flags.String("config", "", "path to the YAML configuration")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *configPath == "" || flags.NArg() != 0 {
			fmt.Fprintln(stderr, "validate requires --config and no positional arguments")
			return 2
		}
		cfg, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintf(stderr, "invalid configuration: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "configuration is valid (%d jobs)\n", len(cfg.Jobs))
		return 0

	case "run":
		flags := flag.NewFlagSet("run", flag.ContinueOnError)
		flags.SetOutput(stderr)
		configPath := flags.String("config", "", "path to the YAML configuration")
		jobName := flags.String("job", "", "run only the named job")
		keepWorkspace := flags.Bool("keep-workspace", false, "retain package job workspaces")
		verbose := flags.Bool("verbose", false, "show git and command output")
		allowCallback := flags.Bool("allow-callback", false, "allow callbacks from trusted configuration")
		environmentConfigPath := flags.String("environment-config", "", "path to a trusted environment policy")
		inheritEnvironment := flags.Bool("inherit-environment", false, "inherit the complete process environment and expand task environment references for trusted repositories")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *configPath == "" || flags.NArg() != 0 {
			fmt.Fprintln(stderr, "run requires --config and no positional arguments")
			return 2
		}
		if *environmentConfigPath != "" && *inheritEnvironment {
			fmt.Fprintln(stderr, "--environment-config and --inherit-environment cannot be used together")
			return 2
		}
		cfg, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintf(stderr, "invalid configuration: %v\n", err)
			return 2
		}

		var environmentPolicy *environmentconfig.Config
		if *environmentConfigPath != "" {
			loaded, err := environmentconfig.Load(*environmentConfigPath)
			if err != nil {
				fmt.Fprintf(stderr, "invalid environment configuration: %v\n", err)
				return 2
			}
			environmentPolicy = &loaded
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		runner := application.Runner{
			KeepWorkspace:      *keepWorkspace,
			AllowCallback:      *allowCallback,
			EnvironmentConfig:  environmentPolicy,
			InheritEnvironment: *inheritEnvironment,
		}
		if *verbose {
			runner.Stdout = stdout
			runner.Stderr = stderr
		}
		results, err := runner.Run(ctx, cfg, *jobName)
		if err != nil {
			fmt.Fprintf(stderr, "cannot run: %v\n", err)
			return 2
		}

		failed := false
		for _, result := range results {
			if result.Successful() {
				if result.Type == config.JobTypeURLs {
					fmt.Fprintf(stdout, "PASS %-20s %d files (%s)\n", result.Name, result.Files, result.Duration.Round(1_000_000))
				} else {
					fmt.Fprintf(stdout, "PASS %-20s (%s)\n", result.Name, result.Duration.Round(1_000_000))
				}
				continue
			}
			failed = true
			fmt.Fprintf(stderr, "FAIL %-20s %v (%s)\n", result.Name, result.Err, result.Duration.Round(1_000_000))
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return 130
		}
		if failed {
			return 1
		}
		return 0

	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

// printUsage 將支援的 CLI 語法寫入指定 writer。
// 輸入為輸出目的 w；輸出透過 writer 產生，函式本身沒有回傳值。
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  artifact-downloader validate --config <file>")
	fmt.Fprintln(w, "  artifact-downloader run --config <file> [--job <name>] [--keep-workspace] [--verbose]")
	fmt.Fprintln(w, "      [--environment-config <file> | --inherit-environment] [--allow-callback]")
}
