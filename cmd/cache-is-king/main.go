package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/zozo123/cache-is-king/internal/bench"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage(stdout)
		return 0
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Fprintln(stdout, version)
		return 0
	}
	if args[0] != "bench" {
		fmt.Fprintf(stderr, "cache-is-king: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
	return runBench(args[1:], stdout, stderr)
}

func runBench(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("cache-is-king bench", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pollute := flags.String("pollute", "", "shell command to run between warm measurements")
	directory := flags.String("dir", ".", "working directory")
	timeout := flags.Duration("timeout", 30*time.Minute, "timeout for each command")
	asJSON := flags.Bool("json", false, "print JSON")
	showOutput := flags.Bool("show-output", false, "stream command output")
	flags.Usage = func() { benchUsage(stderr) }

	separator := -1
	for index, arg := range args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		fmt.Fprintln(stderr, "cache-is-king bench: command must follow --")
		benchUsage(stderr)
		return 2
	}
	if err := flags.Parse(args[:separator]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	command := args[separator+1:]
	if len(command) == 0 {
		fmt.Fprintln(stderr, "cache-is-king bench: empty command")
		return 2
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	report, err := bench.Execute(ctx, bench.Options{
		Command:       command,
		Pollute:       *pollute,
		Directory:     *directory,
		Timeout:       *timeout,
		CommandOutput: *showOutput,
	})
	if err != nil {
		fmt.Fprintf(stderr, "cache-is-king: %v\n", err)
		return 1
	}

	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(stderr, "cache-is-king: %v\n", err)
			return 1
		}
		return 0
	}
	printReport(stdout, report)
	return 0
}

func printReport(output io.Writer, report bench.Report) {
	fmt.Fprintln(output, "CACHE IS KING")
	fmt.Fprintf(output, "command: %s\n", strings.Join(report.Command, " "))
	fmt.Fprintf(output, "cold-ish: %s\n", rounded(report.Cold.Duration))
	fmt.Fprintf(output, "warm:     %s  (%.2fx)\n", rounded(report.Warm.Duration), report.WarmSpeedup)
	if report.Pollution != nil && report.AfterPollution != nil {
		fmt.Fprintf(output, "pollute:  %s\n", rounded(report.Pollution.Duration))
		fmt.Fprintf(output, "after:    %s  (%+.0f%% vs warm)\n", rounded(report.AfterPollution.Duration), report.PollutionPenalty*100)
	}
	fmt.Fprintln(output)
	for _, finding := range report.Findings {
		fmt.Fprintf(output, "- %s\n", finding)
	}
	if report.Warm.Metrics.Available {
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Linux cache signals (warm run):")
		printMetric(output, "file refaults", metric(report.Warm, "workingset_refault_file"))
		printMetric(output, "major faults", metric(report.Warm, "pgmajfault"))
		printMetric(output, "memory PSI", report.Warm.Metrics.PSI["memory"])
		printMetric(output, "I/O PSI", report.Warm.Metrics.PSI["io"])
	}
}

func metric(run bench.Run, name string) int64 {
	if value, ok := run.Metrics.Memory[name]; ok {
		return value
	}
	return run.Metrics.VMStat[name]
}

func printMetric(output io.Writer, name string, value int64) {
	fmt.Fprintf(output, "  %-14s %d\n", name+":", value)
}

func rounded(duration time.Duration) time.Duration {
	if duration >= time.Second {
		return duration.Round(10 * time.Millisecond)
	}
	return duration.Round(time.Millisecond)
}

func usage(output io.Writer) {
	fmt.Fprintln(output, "Cache Is King — measure logical cache reuse and Linux page-cache heat")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintln(output, "  cache-is-king bench [options] -- COMMAND [ARG...]")
	fmt.Fprintln(output, "  cache-is-king version")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Example:")
	fmt.Fprintln(output, "  cache-is-king bench --pollute 'find . -type f -print0 | xargs -0 cat >/dev/null' -- go test ./...")
}

func benchUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: cache-is-king bench [options] -- COMMAND [ARG...]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Options:")
	fmt.Fprintln(output, "  --pollute CMD    run a scan/export-like command before the final warm run")
	fmt.Fprintln(output, "  --dir PATH       working directory (default .)")
	fmt.Fprintln(output, "  --timeout D      timeout for each command (default 30m)")
	fmt.Fprintln(output, "  --show-output    stream command output")
	fmt.Fprintln(output, "  --json           print machine-readable results")
}
