package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

var version = "dev"

type Finding struct {
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Fix      string `json:"fix"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
}
type Doctor struct {
	Kind         string    `json:"kind"`
	Schema       string    `json:"schema"`
	Root         string    `json:"root"`
	Score        int       `json:"score"`
	Grade        string    `json:"grade"`
	Dockerfiles  []string  `json:"dockerfiles"`
	ContextBytes int64     `json:"context_bytes"`
	CacheMounts  int       `json:"cache_mounts"`
	Findings     []Finding `json:"findings"`
}
type Metrics struct {
	Refaults    int64 `json:"refaults"`
	MajorFaults int64 `json:"major_faults"`
	ReadBytes   int64 `json:"read_bytes"`
	MemoryPSIUS int64 `json:"memory_psi_us"`
	IOPSIUS     int64 `json:"io_psi_us"`
}
type Vertex struct {
	Name       string `json:"name"`
	Phase      string `json:"phase"`
	Cached     bool   `json:"cached"`
	DurationMS int64  `json:"duration_ms"`
}
type Build struct {
	Name       string   `json:"name"`
	DurationMS int64    `json:"duration_ms"`
	Vertices   int      `json:"vertices"`
	Cached     int      `json:"cached_vertices"`
	CacheRatio float64  `json:"cache_ratio"`
	Metrics    Metrics  `json:"metrics"`
	Slowest    []Vertex `json:"slowest"`
}
type Bench struct {
	Kind        string         `json:"kind"`
	Schema      string         `json:"schema"`
	Context     string         `json:"context"`
	Builder     string         `json:"builder"`
	Output      string         `json:"output"`
	Score       int            `json:"score"`
	Grade       string         `json:"grade"`
	Cold        Build          `json:"cold"`
	Warm        Build          `json:"warm"`
	Runs        []Build        `json:"runs"`
	WarmSpeedup float64        `json:"warm_speedup"`
	Findings    []Finding      `json:"findings"`
	Environment map[string]any `json:"environment"`
}
type Comparison struct {
	Kind     string    `json:"kind"`
	Schema   string    `json:"schema"`
	Baseline string    `json:"baseline"`
	Winner   string    `json:"winner"`
	Entries  []Entry   `json:"entries"`
	Findings []Finding `json:"findings"`
}
type Entry struct {
	Label       string  `json:"label"`
	WarmMS      int64   `json:"warm_duration_ms"`
	Delta       float64 `json:"duration_delta_pct"`
	CacheRatio  float64 `json:"cache_ratio"`
	Refaults    int64   `json:"refaults"`
	ReadBytes   int64   `json:"read_bytes"`
	MemoryPSIUS int64   `json:"memory_psi_us"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(a []string, out, errout io.Writer) int {
	if len(a) == 0 || a[0] == "help" || a[0] == "--help" {
		usage(out)
		return 0
	}
	switch a[0] {
	case "doctor":
		return runDoctor(a[1:], out, errout)
	case "docker":
		return runDocker(a[1:], out, errout)
	case "compare":
		return runCompare(a[1:], out, errout)
	case "report":
		return runReport(a[1:], out, errout)
	case "env":
		jsonOut(out, environment())
		return 0
	case "version", "--version":
		fmt.Fprintln(out, version)
		return 0
	}
	fmt.Fprintf(errout, "unknown command %q\n", a[0])
	return 2
}
func jsonOut(w io.Writer, v any) { e := json.NewEncoder(w); e.SetIndent("", "  "); _ = e.Encode(v) }
func usage(w io.Writer) {
	fmt.Fprintln(w, "Cache Is King — Docker cache physics from BuildKit keys to Linux page heat\n\nCommands:\n  doctor [--json] [--html FILE] [--strict] [PATH]\n  docker [--runs N] [--output none|load] [--html FILE] [CONTEXT]\n  compare [--json] [--html FILE] LABEL=RESULT.json ...\n  report [--output FILE] RESULT.json\n  env\n  version")
}
