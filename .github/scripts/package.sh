#!/usr/bin/env bash
set -euo pipefail
version="${VERSION:-dev}"
rm -rf dist && mkdir -p dist
for target in "linux amd64" "linux arm64" "darwin amd64" "darwin arm64"; do
  read -r os arch <<< "$target"
  work="$(mktemp -d)"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=${version}" \
    -o "$work/cache-is-king" ./cmd/cache-is-king
  tar -C "$work" -czf "dist/cache-is-king_${os}_${arch}.tar.gz" cache-is-king
  rm -rf "$work"
done
(cd dist && sha256sum cache-is-king_*.tar.gz > checksums.txt)
