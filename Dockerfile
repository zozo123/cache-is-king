# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN --mount=type=cache,id=cik-gomod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=cik-gobuild,target=/root/.cache/go-build,sharing=locked \
    go mod download
COPY cmd ./cmd
ARG VERSION=dev
RUN --mount=type=cache,id=cik-gomod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=cik-gobuild,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/cache-is-king ./cmd/cache-is-king
FROM scratch
COPY --link --from=build /out/cache-is-king /cache-is-king
ENTRYPOINT ["/cache-is-king"]
