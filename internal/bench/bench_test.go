package bench

import (
	"context"
	"testing"
	"time"
)

func TestExecute(t *testing.T) {
	report, err := Execute(context.Background(), Options{
		Command:   []string{"sh", "-c", "printf test >/dev/null"},
		Pollute:   "printf scan >/dev/null",
		Directory: t.TempDir(),
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if report.Cold.ExitCode != 0 || report.Warm.ExitCode != 0 {
		t.Fatalf("unexpected exit codes: cold=%d warm=%d", report.Cold.ExitCode, report.Warm.ExitCode)
	}
	if report.AfterPollution == nil || report.Pollution == nil {
		t.Fatal("expected pollution runs")
	}
	if len(report.Findings) == 0 {
		t.Fatal("expected findings")
	}
}
