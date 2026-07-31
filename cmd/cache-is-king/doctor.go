package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func runDoctor(a []string, out, errout io.Writer) int {
	f := flag.NewFlagSet("doctor", flag.ContinueOnError)
	f.SetOutput(errout)
	js := f.Bool("json", false, "")
	html := f.String("html", "", "")
	strict := f.Bool("strict", false, "")
	if e := f.Parse(a); e != nil {
		return 2
	}
	if f.NArg() > 1 {
		return 2
	}
	root := "."
	if f.NArg() == 1 {
		root = f.Arg(0)
	}
	r, e := analyze(root)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	if *html != "" {
		if e = writeHTML(*html, "Docker cache doctor", r); e != nil {
			fmt.Fprintln(errout, e)
			return 1
		}
	}
	if *js {
		jsonOut(out, r)
	} else {
		printDoctor(out, r)
	}
	if *strict && high(r.Findings) {
		return 1
	}
	return 0
}
func analyze(root string) (Doctor, error) {
	abs, e := filepath.Abs(root)
	if e != nil {
		return Doctor{}, e
	}
	if s, e := os.Stat(abs); e != nil || !s.IsDir() {
		return Doctor{}, fmt.Errorf("not a directory: %s", root)
	}
	r := Doctor{Kind: "doctor", Schema: "cache-is-king/v1", Root: abs, Score: 100}
	var files []string
	filepath.WalkDir(abs, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if p == abs {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "target", "dist", ".venv", ".cache":
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, p)
		if i, e := d.Info(); e == nil {
			r.ContextBytes += i.Size()
		}
		return nil
	})
	ignore := exists(filepath.Join(abs, ".dockerignore"))
	for _, p := range files {
		b := strings.ToLower(filepath.Base(p))
		if b == "dockerfile" || strings.HasPrefix(b, "dockerfile.") || strings.HasSuffix(b, ".dockerfile") {
			rel, _ := filepath.Rel(abs, p)
			rel = filepath.ToSlash(rel)
			r.Dockerfiles = append(r.Dockerfiles, rel)
			x, m, e := inspectDockerfile(p, rel, ignore)
			if e != nil {
				return r, e
			}
			r.Findings = append(r.Findings, x...)
			r.CacheMounts += m
		}
	}
	r.Findings = append(r.Findings, inspectCI(abs, files)...)
	if len(r.Dockerfiles) == 0 {
		r.Findings = append(r.Findings, Finding{"high", "No Dockerfile found", "No discoverable Dockerfile exists in this context.", "Run from the build context root or add a Dockerfile.", "", 0})
	}
	if !ignore {
		sev := "medium"
		if r.ContextBytes > 100<<20 {
			sev = "high"
		}
		r.Findings = append(r.Findings, Finding{sev, "Build context has no .dockerignore", fmt.Sprintf("The repository walk found %s before ignore rules.", size(r.ContextBytes)), "Exclude .git, dependencies, caches, outputs, and test artifacts.", ".dockerignore", 0})
	}
	sortFindings(r.Findings)
	for _, x := range r.Findings {
		switch x.Severity {
		case "high":
			r.Score -= 16
		case "medium":
			r.Score -= 7
		default:
			r.Score -= 2
		}
	}
	if r.Score < 0 {
		r.Score = 0
	}
	r.Grade = grade(r.Score)
	return r, nil
}

type lineRec struct {
	text string
	line int
}

func logical(s string) []lineRec {
	var o []lineRec
	cur := ""
	start := 1
	for i, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if cur == "" {
			start = i + 1
		}
		cur += strings.TrimSuffix(t, "\\") + " "
		if !strings.HasSuffix(t, "\\") {
			o = append(o, lineRec{strings.TrimSpace(cur), start})
			cur = ""
		}
	}
	return o
}
func inspectDockerfile(path, rel string, ignore bool) ([]Finding, int, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, 0, e
	}
	var out []Finding
	broad, manifest := false, false
	mounts := 0
	for _, l := range logical(string(b)) {
		p := strings.Fields(l.text)
		if len(p) == 0 {
			continue
		}
		op := strings.ToUpper(p[0])
		arg := strings.TrimSpace(strings.TrimPrefix(l.text, p[0]))
		low := strings.ToLower(arg)
		switch op {
		case "FROM":
			broad = false
			manifest = false
			if !strings.Contains(arg, "@sha256:") && strings.Contains(strings.Fields(arg)[0], ":latest") {
				out = append(out, Finding{"low", "Base image uses latest", "A moving base invalidates the whole graph and makes measurements noisy.", "Pin a version or digest.", rel, l.line})
			}
		case "COPY", "ADD":
			if broadCopy(arg) {
				broad = true
				if !ignore {
					out = append(out, Finding{"high", "Broad COPY has no .dockerignore protection", "COPY . expands cache keys to the entire context.", "Add .dockerignore and narrow COPY inputs.", rel, l.line})
				}
			}
			if manifestCopy(low) {
				manifest = true
			}
		case "RUN":
			if strings.Contains(low, "--mount=type=cache") {
				mounts++
			}
			if dependency(low) && broad && !manifest {
				out = append(out, Finding{"high", "Source changes invalidate dependency installation", "A broad source copy appears before dependency installation.", "Copy manifests first, install dependencies, then copy source.", rel, l.line})
			}
			if dependency(low) && !strings.Contains(low, "--mount=type=cache") {
				out = append(out, Finding{"medium", "Dependency or compiler state has no cache mount", "Layer reuse cannot preserve mutable package/compiler state independently.", "Add named BuildKit cache mounts; use sharing=locked where needed.", rel, l.line})
			}
		case "ARG", "ENV":
			if containsAny(low, "token", "password", "secret", "api_key", "private_key", "credential") {
				out = append(out, Finding{"high", "Secret-like value enters image metadata", "ARG and ENV can leak credentials and secret rotation does not invalidate cached steps.", "Use RUN --mount=type=secret or type=ssh.", rel, l.line})
			}
		}
	}
	return out, mounts, nil
}
func broadCopy(s string) bool {
	f := strings.Fields(strings.ToLower(s))
	return len(f) >= 2 && f[0] == "." && (f[len(f)-1] == "." || f[len(f)-1] == "./")
}
func manifestCopy(s string) bool {
	return containsAny(s, "go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock", "yarn.lock", "cargo.toml", "cargo.lock", "requirements.txt", "pyproject.toml", "pom.xml", "gradle")
}
func dependency(s string) bool {
	return containsAny(s, "go mod download", "go build", "go test", "npm ci", "npm install", "pnpm install", "yarn install", "cargo build", "cargo test", "pip install", "poetry install", "mvn ", "gradle ", "apt-get install", "apk add")
}
func inspectCI(root string, files []string) []Finding {
	var o []Finding
	actions := 0
	var candidates []string
	for _, p := range files {
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, ".github/workflows/") || (!strings.HasSuffix(rel, ".yml") && !strings.HasSuffix(rel, ".yaml")) {
			continue
		}
		b, _ := os.ReadFile(p)
		s := strings.ToLower(string(b))
		if strings.Contains(s, "docker/build-push-action@") {
			actions += strings.Count(s, "docker/build-push-action@")
			candidates = append(candidates, rel)
			if !strings.Contains(s, "cache-from:") && !strings.Contains(s, "cache-to:") {
				o = append(o, Finding{"medium", "GitHub Docker build has no portable layer cache", "Hosted runners commonly start with an empty BuildKit store.", "Add cache-from/cache-to or use persistent builder storage.", rel, 0})
			}
			if strings.Contains(s, "type=gha") && !strings.Contains(s, "mode=max") {
				o = append(o, Finding{"medium", "GHA export omits intermediate results", "type=gha without mode=max may preserve less of a multi-stage graph.", "Use mode=max when intermediate reuse justifies transfer cost.", rel, 0})
			}
			if strings.Contains(s, "load: true") && strings.Contains(s, "push: true") {
				o = append(o, Finding{"high", "Image is loaded and pushed", "The image can be serialized through both the local store and registry.", "Push directly unless a later local step consumes the image.", rel, 0})
			}
		}
	}
	if actions > 1 {
		for _, rel := range candidates {
			b, _ := os.ReadFile(filepath.Join(root, rel))
			s := strings.ToLower(string(b))
			if strings.Contains(s, "type=gha") && !strings.Contains(s, "scope=") {
				o = append(o, Finding{"high", "Multiple images share the default GHA cache scope", "Unrelated image caches may overwrite each other.", "Scope cache keys by image lineage and import a main-branch fallback.", rel, 0})
				break
			}
		}
	}
	return o
}
