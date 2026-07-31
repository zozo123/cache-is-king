# Cache Is King

[![CI](https://github.com/zozo123/cache-is-king/actions/workflows/ci.yml/badge.svg)](https://github.com/zozo123/cache-is-king/actions/workflows/ci.yml)
[![Pages](https://img.shields.io/badge/live-GitHub%20Pages-64f0a7)](https://zozo123.github.io/cache-is-king/)

**Your cache hit. Your disk did not.**

`cache-is-king` is a local-first Docker cache profiler. It connects Dockerfile and BuildKit cache reuse to the hidden layer that decides whether a hit is actually fast: Linux page-cache residency.

It answers three questions:

1. Which Dockerfile or CI choice destroys reuse?
2. Which BuildKit phase dominates cold and warm builds?
3. Did a logical cache hit still refault pages and reread storage?

No account, daemon, source upload, root, or custom kernel is required.

## Start

```sh
curl -fsSL https://zozo123.github.io/cache-is-king/run | sh -s -- doctor .
```

Then benchmark Docker:

```sh
cache-is-king docker --runs 3 --json . > default.json
cache-is-king docker --runs 3 --html cache-report.html .
```

## `doctor`

```sh
cache-is-king doctor .
cache-is-king doctor --strict .
cache-is-king doctor --json . > doctor.json
```

It finds high-value cache mistakes:

- broad `COPY . .` before dependency installation;
- missing `.dockerignore`;
- compiler or package-manager steps without cache mounts;
- secrets passed through `ARG` or `ENV`;
- hosted GitHub builds without portable layer caches;
- `type=gha` without `mode=max`;
- unrelated images sharing the default GHA cache scope;
- `load: true` plus `push: true` double handling.

## `docker`

```sh
cache-is-king docker .
cache-is-king docker --runs 5 --output load --tag app:probe .
```

The command creates an isolated `docker-container` Buildx builder, runs one cold and multiple warm builds, parses BuildKit's raw JSON progress, and records:

- completed and cached vertices;
- context, metadata, execution, cache-transfer, and export phases;
- `workingset_refault_file` and major faults;
- cgroup storage reads;
- memory and I/O PSI.

A **logical hit** means BuildKit can reuse a result. A **hot hit** means the bytes needed to realize it are still resident near the CPU. Cache Is King measures the gap.

## Evaluate `cache_ext`

[`cache_ext`](https://github.com/cache-ext/cache_ext) lets controlled Linux hosts attach custom eBPF page-cache eviction policies to cgroups. Cache Is King provides the Docker workload and stable report format for comparing those policies:

```sh
# Run the same command under each policy/environment.
cache-is-king docker --runs 5 --json . > default.json
cache-is-king docker --runs 5 --json . > s3fifo.json
cache-is-king docker --runs 5 --json . > lhd.json

cache-is-king compare --html policies.html \
  default=default.json s3fifo=s3fifo.json lhd=lhd.json
```

The comparison keeps warm duration beside cache ratio, refaults, storage reads, and PSI so a policy is not credited merely for receiving warmer BuildKit state.

## Commands

```text
cache-is-king doctor [--json] [--html FILE] [--strict] [PATH]
cache-is-king docker [--runs N] [--output none|load] [--html FILE] [CONTEXT]
cache-is-king compare [--json] [--html FILE] LABEL=RESULT.json ...
cache-is-king report [--output FILE] RESULT.json
cache-is-king env
```

## Install

```sh
go install github.com/zozo123/cache-is-king/cmd/cache-is-king@latest
```

The GitHub Pages installer downloads checksum-verified release archives and falls back to `go run` before the first release.

## Method and limits

- The isolated builder makes BuildKit state cold, but registries and host storage may already be warm.
- The tool never calls `drop_caches`.
- BuildKit vertices overlap; phase time is diagnostic, not an exact wall-time partition.
- Linux metrics are strongest when the binary runs in the same cgroup/VM as the Docker workload.
- macOS and Windows builds work, but kernel counters belong to the Linux Docker VM only when measured there.

Inspired by [The physics of Docker build caching](https://www.blacksmith.sh/blog/the-physics-of-docker-build-caching), [Cache is King](https://arxiv.org/abs/2502.02750), and [`cache_ext`](https://github.com/cache-ext/cache_ext).

## Develop

```sh
go test ./...
go vet ./...
go run ./cmd/cache-is-king doctor .
```

MIT.
