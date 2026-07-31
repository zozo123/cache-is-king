package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func runDocker(a []string, out, errout io.Writer) int {
	f := flag.NewFlagSet("docker", flag.ContinueOnError)
	f.SetOutput(errout)
	runs := f.Int("runs", 2, "")
	file := f.String("file", "Dockerfile", "")
	builder := f.String("builder", "", "")
	fresh := f.Bool("fresh-builder", true, "")
	keep := f.Bool("keep-builder", false, "")
	output := f.String("output", "none", "")
	tag := f.String("tag", "cache-is-king:probe", "")
	js := f.Bool("json", false, "")
	html := f.String("html", "", "")
	show := f.Bool("show-output", false, "")
	timeout := f.Duration("timeout", 45*time.Minute, "")
	if f.Parse(a) != nil || f.NArg() > 1 || *runs < 2 {
		return 2
	}
	root := "."
	if f.NArg() == 1 {
		root = f.Arg(0)
	}
	r, e := benchmark(context.Background(), root, *file, *builder, *fresh, *keep, *runs, *output, *tag, *show, *timeout)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	if *html != "" {
		writeHTML(*html, "Docker cache physics", r)
	}
	if *js {
		jsonOut(out, r)
	} else {
		printBench(out, r)
	}
	return 0
}
func benchmark(parent context.Context, root, file, builder string, fresh, keep bool, runs int, output, tag string, show bool, timeout time.Duration) (Bench, error) {
	if _, e := exec.LookPath("docker"); e != nil {
		return Bench{}, fmt.Errorf("docker is required")
	}
	abs, _ := filepath.Abs(root)
	if _, e := os.Stat(filepath.Join(abs, file)); e != nil {
		return Bench{}, e
	}
	created := ""
	if builder == "" && fresh {
		created = fmt.Sprintf("cik-%d", time.Now().UnixNano())
		c := exec.Command("docker", "buildx", "create", "--name", created, "--driver", "docker-container", "--use")
		if b, e := c.CombinedOutput(); e != nil {
			return Bench{}, fmt.Errorf("create builder: %s", strings.TrimSpace(string(b)))
		}
		builder = created
		exec.Command("docker", "buildx", "inspect", "--bootstrap", builder).Run()
		if !keep {
			defer exec.Command("docker", "buildx", "rm", "-f", builder).Run()
		}
	}
	r := Bench{Kind: "docker-benchmark", Schema: "cache-is-king/v1", Context: abs, Builder: builder, Output: output, Environment: environment()}
	for i := 0; i < runs; i++ {
		ctx, cancel := context.WithTimeout(parent, timeout)
		name := fmt.Sprintf("warm-%d", i)
		if i == 0 {
			name = "cold"
		}
		x, e := buildOnce(ctx, abs, file, builder, output, tag, name, show)
		cancel()
		if e != nil {
			return r, e
		}
		r.Runs = append(r.Runs, x)
	}
	r.Cold = r.Runs[0]
	r.Warm = r.Runs[1]
	for _, x := range r.Runs[1:] {
		if x.DurationMS < r.Warm.DurationMS {
			r.Warm = x
		}
	}
	r.WarmSpeedup = float64(r.Cold.DurationMS) / float64(r.Warm.DurationMS)
	r.Findings = benchFindings(r)
	r.Score = 100
	for _, x := range r.Findings {
		if x.Severity == "high" {
			r.Score -= 18
		} else if x.Severity == "medium" {
			r.Score -= 8
		} else {
			r.Score -= 2
		}
	}
	if r.Score < 0 {
		r.Score = 0
	}
	r.Grade = grade(r.Score)
	return r, nil
}
func buildOnce(ctx context.Context, root, file, builder, output, tag, name string, show bool) (Build, error) {
	a := []string{"buildx", "build", "--progress=rawjson", "--file", file}
	if builder != "" {
		a = append(a, "--builder", builder)
	}
	if output == "load" {
		a = append(a, "--load", "--tag", tag)
	} else {
		a = append(a, "--output", "type=cacheonly")
	}
	a = append(a, ".")
	cmd := exec.CommandContext(ctx, "docker", a...)
	cmd.Dir = root
	var buf bytes.Buffer
	if show {
		cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
		cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	} else {
		cmd.Stdout = &buf
		cmd.Stderr = &buf
	}
	before := snapshotNow()
	start := time.Now()
	e := cmd.Run()
	elapsed := time.Since(start)
	after := snapshotNow()
	if e != nil {
		return Build{}, fmt.Errorf("%s build failed: %w\n%s", name, e, tail(buf.String(), 25))
	}
	all := vertices(buf.Bytes())
	cached := 0
	for _, x := range all {
		if x.Cached {
			cached++
		}
	}
	slow := append([]Vertex(nil), all...)
	sort.Slice(slow, func(i, j int) bool { return slow[i].DurationMS > slow[j].DurationMS })
	if len(slow) > 8 {
		slow = slow[:8]
	}
	ratio := 0.0
	if len(all) > 0 {
		ratio = float64(cached) / float64(len(all))
	}
	return Build{name, elapsed.Milliseconds(), len(all), cached, ratio, snapshotDiff(before, after), slow}, nil
}

type rawVertex struct {
	ID        string `json:"digest"`
	Name      string `json:"name"`
	Started   string `json:"started"`
	Completed string `json:"completed"`
	Cached    bool   `json:"cached"`
}

func vertices(b []byte) []Vertex {
	m := map[string]rawVertex{}
	s := bufio.NewScanner(bytes.NewReader(b))
	s.Buffer(make([]byte, 4096), 4<<20)
	for s.Scan() {
		var e struct {
			Vertex *rawVertex `json:"vertex"`
		}
		if json.Unmarshal(s.Bytes(), &e) == nil && e.Vertex != nil {
			x := m[e.Vertex.ID]
			if e.Vertex.Name != "" {
				x.Name = e.Vertex.Name
			}
			if e.Vertex.Started != "" {
				x.Started = e.Vertex.Started
			}
			if e.Vertex.Completed != "" {
				x.Completed = e.Vertex.Completed
			}
			x.ID = e.Vertex.ID
			x.Cached = x.Cached || e.Vertex.Cached
			m[x.ID] = x
		}
	}
	var o []Vertex
	for _, x := range m {
		if x.Completed == "" {
			continue
		}
		a, _ := time.Parse(time.RFC3339Nano, x.Started)
		z, _ := time.Parse(time.RFC3339Nano, x.Completed)
		d := int64(0)
		if !a.IsZero() && !z.IsZero() {
			d = z.Sub(a).Milliseconds()
		}
		o = append(o, Vertex{x.Name, phase(x.Name), x.Cached, d})
	}
	return o
}
func phase(s string) string {
	l := strings.ToLower(s)
	switch {
	case containsAny(l, "exporting", "writing image", "importing to docker"):
		return "export"
	case containsAny(l, "load build context", "transferring context"):
		return "context"
	case containsAny(l, "cache manifest", "cache config"):
		return "cache-transfer"
	case containsAny(l, "load metadata", "resolve image config"):
		return "metadata"
	}
	return "execution"
}
func benchFindings(r Bench) []Finding {
	var f []Finding
	w := r.Warm
	if r.WarmSpeedup < 1.25 {
		f = append(f, Finding{"high", "Warm build barely improves", "BuildKit reuse is not removing meaningful work.", "Run doctor and inspect slow uncached vertices.", "", 0})
	}
	if w.CacheRatio < .7 {
		f = append(f, Finding{"high", "Warm vertex hit rate is low", fmt.Sprintf("Only %.0f%% of completed vertices are cached.", w.CacheRatio*100), "Stabilize inputs and separate dependency manifests from source.", "", 0})
	}
	if w.CacheRatio > .8 && w.Metrics.Refaults > 1000 {
		f = append(f, Finding{"high", "Logical hits are not physically hot", fmt.Sprintf("The warm build cached %.0f%% of vertices but refaulted %d file pages.", w.CacheRatio*100, w.Metrics.Refaults), "Compare runner memory/page policy and isolate scan/export work.", "", 0})
	}
	if w.Metrics.ReadBytes > 256<<20 {
		f = append(f, Finding{"medium", "Warm build rereads substantial storage", fmt.Sprintf("The warm build read %s from block devices.", size(w.Metrics.ReadBytes)), "Inspect cache mounts, image loading, and page-cache residency.", "", 0})
	}
	for _, v := range w.Slowest {
		if v.Phase == "export" && v.DurationMS*3 > w.DurationMS {
			f = append(f, Finding{"high", "Export dominates the warm build", fmt.Sprintf("%q consumed %s.", v.Name, duration(v.DurationMS)), "Avoid --load when no local consumer needs the image; push directly.", "", 0})
			break
		}
	}
	return f
}
func runCompare(a []string, out, errout io.Writer) int {
	f := flag.NewFlagSet("compare", flag.ContinueOnError)
	f.SetOutput(errout)
	js := f.Bool("json", false, "")
	html := f.String("html", "", "")
	if f.Parse(a) != nil || f.NArg() < 2 {
		return 2
	}
	var entries []Entry
	for _, arg := range f.Args() {
		label, path := split(arg)
		b, e := os.ReadFile(path)
		if e != nil {
			fmt.Fprintln(errout, e)
			return 1
		}
		var r Bench
		if json.Unmarshal(b, &r) != nil || r.Kind != "docker-benchmark" {
			fmt.Fprintf(errout, "invalid benchmark: %s\n", path)
			return 1
		}
		entries = append(entries, Entry{label, r.Warm.DurationMS, 0, r.Warm.CacheRatio, r.Warm.Metrics.Refaults, r.Warm.Metrics.ReadBytes, r.Warm.Metrics.MemoryPSIUS})
	}
	base := entries[0].WarmMS
	for i := range entries {
		entries[i].Delta = float64(entries[i].WarmMS-base) / float64(base) * 100
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].WarmMS < entries[j].WarmMS })
	r := Comparison{"comparison", "cache-is-king/v1", splitLabel(f.Args()[0]), entries[0].Label, entries, nil}
	if entries[0].Delta < -5 {
		r.Findings = []Finding{{"medium", fmt.Sprintf("%s is %.0f%% faster than baseline", entries[0].Label, -entries[0].Delta), "Compare cache ratio, refaults, reads, and repeated trials before attributing the gain to page policy.", "Repeat with identical builder state and workload inputs.", "", 0}}
	}
	if *html != "" {
		writeHTML(*html, "Page-cache policy comparison", r)
	}
	if *js {
		jsonOut(out, r)
	} else {
		printCompare(out, r)
	}
	return 0
}
