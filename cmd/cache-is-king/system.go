package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

func runReport(a []string, out, errout io.Writer) int {
	f := flag.NewFlagSet("report", flag.ContinueOnError)
	f.SetOutput(errout)
	dest := f.String("output", "cache-report.html", "")
	if f.Parse(a) != nil || f.NArg() != 1 {
		return 2
	}
	b, e := os.ReadFile(f.Arg(0))
	if e != nil {
		return 1
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return 1
	}
	if writeHTML(*dest, "Cache Is King report", v) != nil {
		return 1
	}
	fmt.Fprintln(out, *dest)
	return 0
}

type snap struct{ refault, major, read, mpsi, ipsi int64 }

func snapshotNow() snap {
	if runtime.GOOS != "linux" {
		return snap{}
	}
	root := cgroupRoot()
	m := kv(filepath.Join(root, "memory.stat"))
	i := ioStats(filepath.Join(root, "io.stat"))
	return snap{m["workingset_refault_file"], m["pgmajfault"], i["rbytes"], psi(filepath.Join(root, "memory.pressure")), psi(filepath.Join(root, "io.pressure"))}
}
func snapshotDiff(a, b snap) Metrics {
	return Metrics{b.refault - a.refault, b.major - a.major, b.read - a.read, b.mpsi - a.mpsi, b.ipsi - a.ipsi}
}
func cgroupRoot() string {
	b, _ := os.ReadFile("/proc/self/cgroup")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "0::") {
			return filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(l, "0::"))
		}
	}
	return "/sys/fs/cgroup"
}
func kv(p string) map[string]int64 {
	m := map[string]int64{}
	f, e := os.Open(p)
	if e != nil {
		return m
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		x := strings.Fields(s.Text())
		if len(x) > 1 {
			m[x[0]], _ = strconv.ParseInt(x[1], 10, 64)
		}
	}
	return m
}
func ioStats(p string) map[string]int64 {
	m := map[string]int64{}
	f, e := os.Open(p)
	if e != nil {
		return m
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		x := strings.Fields(s.Text())
		for _, v := range x[1:] {
			q := strings.SplitN(v, "=", 2)
			if len(q) == 2 {
				n, _ := strconv.ParseInt(q[1], 10, 64)
				m[q[0]] += n
			}
		}
	}
	return m
}
func psi(p string) int64 {
	f, e := os.Open(p)
	if e != nil {
		return 0
	}
	defer f.Close()
	var n int64
	s := bufio.NewScanner(f)
	for s.Scan() {
		for _, x := range strings.Fields(s.Text()) {
			if strings.HasPrefix(x, "total=") {
				v, _ := strconv.ParseInt(strings.TrimPrefix(x, "total="), 10, 64)
				n += v
			}
		}
	}
	return n
}
func environment() map[string]any {
	m := map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "cgroup_v2": exists("/sys/fs/cgroup/cgroup.controllers"), "psi": exists("/proc/pressure/memory"), "cache_ext": exists("/sys/fs/bpf/cache_ext"), "kernel": cmd("uname", "-r"), "docker": cmd("docker", "version", "--format", "{{.Server.Version}}"), "buildx": cmd("docker", "buildx", "version")}
	if b, e := os.ReadFile("/sys/kernel/mm/lru_gen/enabled"); e == nil {
		m["mglru"] = strings.TrimSpace(string(b))
	}
	return m
}
func writeHTML(path, title string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	t := template.Must(template.New("r").Parse(`<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width"><title>{{.T}}</title><style>body{margin:0;background:#07100d;color:#effff6;font:15px/1.55 system-ui}main{max-width:1050px;margin:auto;padding:48px 20px}h1{font-size:clamp(38px,7vw,74px);letter-spacing:-.05em}.g{color:#64f0a7}pre{white-space:pre-wrap;background:#0e1d17;border:1px solid #294537;border-radius:16px;padding:22px;color:#bfe8d0}</style><main><div class=g>CACHE IS KING · LOCAL REPORT</div><h1>{{.T}}</h1><pre id=o></pre></main><script>const r={{.J}};o.textContent=JSON.stringify(r,null,2)</script>`))
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return t.Execute(f, map[string]any{"T": title, "J": template.JS(b)})
}
func printDoctor(w io.Writer, r Doctor) {
	fmt.Fprintf(w, "CACHE IS KING · DOCKER DOCTOR   %d/100 (%s)\ndockerfiles: %d   context: %s   cache mounts: %d\n\n", r.Score, r.Grade, len(r.Dockerfiles), size(r.ContextBytes), r.CacheMounts)
	printFindings(w, r.Findings)
}
func printBench(w io.Writer, r Bench) {
	fmt.Fprintf(w, "CACHE IS KING · DOCKER PHYSICS   %d/100 (%s)\ncold: %s   warm: %s   speedup: %.2fx   cached vertices: %.0f%%\nrefaults: %d   reads: %s   memory PSI: %s\n\n", r.Score, r.Grade, duration(r.Cold.DurationMS), duration(r.Warm.DurationMS), r.WarmSpeedup, r.Warm.CacheRatio*100, r.Warm.Metrics.Refaults, size(r.Warm.Metrics.ReadBytes), duration(r.Warm.Metrics.MemoryPSIUS/1000))
	printFindings(w, r.Findings)
}
func printCompare(w io.Writer, r Comparison) {
	fmt.Fprintf(w, "CACHE IS KING · POLICY COMPARISON   baseline=%s   winner=%s\n\n", r.Baseline, r.Winner)
	for _, x := range r.Entries {
		fmt.Fprintf(w, "%-18s %9s %+7.0f%% cached=%3.0f%% refaults=%d reads=%s\n", x.Label, duration(x.WarmMS), x.Delta, x.CacheRatio*100, x.Refaults, size(x.ReadBytes))
	}
	fmt.Fprintln(w)
	printFindings(w, r.Findings)
}
func printFindings(w io.Writer, f []Finding) {
	if len(f) == 0 {
		fmt.Fprintln(w, "No material cache issues found.")
		return
	}
	for _, x := range f {
		loc := ""
		if x.File != "" {
			loc = " · " + x.File
			if x.Line > 0 {
				loc += fmt.Sprintf(":%d", x.Line)
			}
		}
		fmt.Fprintf(w, "[%s] %s%s\n  %s\n  fix: %s\n\n", strings.ToUpper(x.Severity), x.Title, loc, x.Detail, x.Fix)
	}
}
func sortFindings(f []Finding) {
	rank := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(f, func(i, j int) bool { return rank[f[i].Severity] < rank[f[j].Severity] })
}
func containsAny(s string, x ...string) bool {
	for _, v := range x {
		if strings.Contains(s, v) {
			return true
		}
	}
	return false
}
func high(f []Finding) bool {
	for _, x := range f {
		if x.Severity == "high" {
			return true
		}
	}
	return false
}
func grade(n int) string {
	if n >= 90 {
		return "A"
	}
	if n >= 80 {
		return "B"
	}
	if n >= 70 {
		return "C"
	}
	if n >= 60 {
		return "D"
	}
	return "F"
}
func size(n int64) string {
	u := []string{"B", "KiB", "MiB", "GiB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(u)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", v, u[i])
}
func duration(ms int64) string {
	return (time.Duration(ms) * time.Millisecond).Round(time.Millisecond).String()
}
func exists(p string) bool { _, e := os.Stat(p); return e == nil }
func cmd(n string, a ...string) string {
	b, e := exec.Command(n, a...).CombinedOutput()
	if e != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(b))
}
func tail(s string, n int) string {
	x := strings.Split(s, "\n")
	if len(x) > n {
		x = x[len(x)-n:]
	}
	return strings.Join(x, "\n")
}
func split(s string) (string, string) {
	if i := strings.Index(s, "="); i > 0 {
		return s[:i], s[i+1:]
	}
	return strings.TrimSuffix(filepath.Base(s), filepath.Ext(s)), s
}
func splitLabel(s string) string { x, _ := split(s); return x }
