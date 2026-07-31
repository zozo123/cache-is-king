# Cache Is King

**Measure the cache you think you have.**

`cache-is-king` is a small, local-first profiler for build caches. It runs the same command cold-ish and warm, optionally inserts a large scan/export workload, then reports whether persistent cache state actually stayed fast and memory-resident.

It connects two ideas:

- BuildKit and compiler caches decide whether work can be reused.
- Linux page-cache policy decides whether that reuse is cheap or requires storage reads and refaults.

The tool works without root. On Linux it also records cgroup and kernel signals such as file-page refaults, major faults, reclaim activity, and memory/I/O PSI.

## The one-liner

```sh
go run ./cmd/cache-is-king bench \
  --pollute 'find . -type f -print0 | xargs -0 cat >/dev/null' \
  -- go test ./...
```

Example output:

```text
CACHE IS KING
command: go test ./...
cold-ish: 31.42s
warm:      4.18s  (7.52x)
pollute:   8.31s
after:     6.02s  (+44% vs warm)

- warm execution is 7.52x faster; the workload has meaningful reusable state
- scan pollution slowed the warm command by 44%; persistent bytes are not staying memory-hot
- warm execution still incurred 18342 cgroup file-page refaults
```

## Useful experiments

### Go compiler cache

```sh
go run ./cmd/cache-is-king bench -- go test ./...
```

### Docker build plus context pollution

```sh
go run ./cmd/cache-is-king bench \
  --pollute 'find . -type f -print0 | xargs -0 cat >/dev/null' \
  -- docker buildx build --load .
```

### Large image export as the polluter

```sh
go run ./cmd/cache-is-king bench \
  --pollute 'docker save my-large-image:latest >/dev/null' \
  -- go test ./...
```

### Machine-readable experiment

```sh
go run ./cmd/cache-is-king bench --json -- go test ./... > result.json
```

The JSON format is intentionally suitable for comparing:

- default Linux page-cache behavior;
- cgroup memory protection;
- `posix_fadvise`-aware exporters;
- persistent disks with cold guest RAM;
- custom `cache_ext` policies;
- runner sizes and BuildKit concurrency levels.

## What it measures

| Signal | Meaning |
| --- | --- |
| Cold-ish duration | First measured execution in the current environment |
| Warm duration | Immediate repeated execution |
| Warm speedup | Cold-ish duration divided by warm duration |
| Pollution penalty | Final duration relative to the warm baseline |
| `workingset_refault_file` | File pages that were evicted and needed again |
| `pgmajfault` | Faults requiring storage access |
| Memory PSI | Time tasks stalled under memory pressure |
| I/O PSI | Time tasks stalled on storage |

This is not a laboratory-grade cold-cache harness: it never calls `drop_caches`, and the first run may inherit existing machine state. That is deliberate. It evaluates the cache behavior developers and CI runners actually experience without requiring privileged access.

## Product direction

The next useful layers are:

1. **Repository doctor** — detect Go, Rust, Bazel, BuildKit, package-manager, and CI cache configuration; suggest a precise experiment.
2. **BuildKit trace correlation** — map refault and PSI deltas to LLB vertices such as compilation, snapshotting, export, and image loading.
3. **Experiment matrix** — run memory, concurrency, and cache-policy sweeps and compare JSON reports.
4. **Hot-set manifest** — identify the small high-value file set worth prefetching when a persistent disk is attached to a fresh VM.
5. **`cache_ext` adapter** — execute the same workload under multiple page-cache policies and rank them by build time, refault tax, and tail latency.

The long-term goal is an open benchmark and profiler for answering a deceptively simple question:

> A cache hit occurred—but where did the bytes come from, and was the hit actually fast?

## Develop

Requires Go 1.24 or newer.

```sh
go test ./...
go run ./cmd/cache-is-king bench -- go test ./...
```

## Relationship to Wasted Cycles

[`wasted-cycles`](https://github.com/zozo123/wasted-cycles) finds machine time blocking agent work. `cache-is-king` explains one important cause: why builds remain slow even when logical caches exist.

A future integration can let Wasted Cycles identify a repeated slow build and launch a Cache Is King experiment automatically.

## License

MIT
