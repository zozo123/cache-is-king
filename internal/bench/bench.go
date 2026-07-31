package bench

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/zozo123/cache-is-king/internal/metrics"
)

type Options struct {
	Command       []string
	Pollute       string
	Directory     string
	Timeout       time.Duration
	CommandOutput bool
}

type Run struct {
	Name       string        `json:"name"`
	Command    []string      `json:"command"`
	Duration   time.Duration `json:"duration"`
	ExitCode   int           `json:"exit_code"`
	Metrics    metrics.Delta `json:"metrics"`
}

type Report struct {
	Command          []string  `json:"command"`
	PollutionCommand string    `json:"pollution_command,omitempty"`
	OS               string    `json:"os"`
	Arch             string    `json:"arch"`
	Cold             Run       `json:"cold"`
	Warm             Run       `json:"warm"`
	Pollution         *Run      `json:"pollution,omitempty"`
	AfterPollution    *Run      `json:"after_pollution,omitempty"`
	WarmSpeedup       float64   `json:"warm_speedup"`
	PollutionPenalty  float64   `json:"pollution_penalty,omitempty"`
	Findings          []string  `json:"findings"`
}

func Execute(ctx context.Context, options Options) (Report, error) {
	if len(options.Command) == 0 {
		return Report{}, fmt.Errorf("no command supplied")
	}
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Minute
	}

	report := Report{
		Command:          options.Command,
		PollutionCommand: options.Pollute,
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
	}

	var err error
	report.Cold, err = run(ctx, options, "cold-ish", options.Command, false)
	if err != nil {
		return report, err
	}
	report.Warm, err = run(ctx, options, "warm", options.Command, false)
	if err != nil {
		return report, err
	}

	if report.Warm.Duration > 0 {
		report.WarmSpeedup = float64(report.Cold.Duration) / float64(report.Warm.Duration)
	}

	if options.Pollute != "" {
		pollution, pollutionErr := run(ctx, options, "pollution", []string{options.Pollute}, true)
		report.Pollution = &pollution
		if pollutionErr != nil {
			return report, pollutionErr
		}
		after, afterErr := run(ctx, options, "after-pollution", options.Command, false)
		report.AfterPollution = &after
		if afterErr != nil {
			return report, afterErr
		}
		if report.Warm.Duration > 0 {
			report.PollutionPenalty = float64(after.Duration-report.Warm.Duration) / float64(report.Warm.Duration)
		}
	}

	report.Findings = findings(report)
	return report, nil
}

func run(parent context.Context, options Options, name string, command []string, shell bool) (Run, error) {
	ctx, cancel := context.WithTimeout(parent, options.Timeout)
	defer cancel()

	var cmd *exec.Cmd
	if shell {
		cmd = exec.CommandContext(ctx, "sh", "-c", command[0])
	} else {
		cmd = exec.CommandContext(ctx, command[0], command[1:]...)
	}
	cmd.Dir = options.Directory
	cmd.Env = os.Environ()
	if options.CommandOutput {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	before := metrics.Read()
	started := time.Now()
	err := cmd.Run()
	elapsed := time.Since(started)
	after := metrics.Read()

	result := Run{
		Name:     name,
		Command:  command,
		Duration: elapsed,
		ExitCode: 0,
		Metrics:  metrics.Between(before, after),
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else if ctx.Err() != nil {
			result.ExitCode = 124
		} else {
			result.ExitCode = 1
		}
		return result, fmt.Errorf("%s run failed: %w", name, err)
	}
	return result, nil
}

func findings(report Report) []string {
	out := make([]string, 0, 4)
	if report.WarmSpeedup >= 1.25 {
		out = append(out, fmt.Sprintf("warm execution is %.2fx faster; the workload has meaningful reusable state", report.WarmSpeedup))
	} else {
		out = append(out, "warm execution improved little; the workload may be CPU-bound, incorrectly cached, or already dominated by non-cache work")
	}

	if report.AfterPollution != nil {
		switch {
		case report.PollutionPenalty >= 0.20:
			out = append(out, fmt.Sprintf("scan pollution slowed the warm command by %.0f%%; persistent bytes are not staying memory-hot", report.PollutionPenalty*100))
		case report.PollutionPenalty >= 0.05:
			out = append(out, fmt.Sprintf("scan pollution caused a measurable %.0f%% warm-build penalty", report.PollutionPenalty*100))
		default:
			out = append(out, "the selected pollution workload did not materially damage the warm result")
		}
	}

	if value := report.Warm.Metrics.Memory["workingset_refault_file"]; value > 0 {
		out = append(out, fmt.Sprintf("warm execution still incurred %d cgroup file-page refaults", value))
	} else if value := report.Warm.Metrics.VMStat["workingset_refault_file"]; value > 0 {
		out = append(out, fmt.Sprintf("system-wide file-page refaults increased by %d during the warm execution", value))
	}
	return out
}
