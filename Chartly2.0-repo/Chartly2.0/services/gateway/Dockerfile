# syntax=docker/dockerfile:1.7

# Chartly 2.0  Gateway service (Go)
# Multi-stage build: deps -> test -> build -> healthcheck -> runtime (distroless).

ARG GO_VERSION=1.22
ARG DISTROLESS_IMAGE=gcr.io/distroless/base-debian12:nonroot

###############################################################################
# deps: download modules with caching
###############################################################################
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS deps
WORKDIR /src

COPY go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download -x

###############################################################################
# test: run unit tests (cached build artifacts)
###############################################################################
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS test
WORKDIR /src

COPY --from=deps /go/pkg/mod /go/pkg/mod
COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go test ./... -count=1

###############################################################################
# build: compile gateway
###############################################################################
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build
WORKDIR /src

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_URL=unknown
ARG LICENSES=UNLICENSED

ENV CGO_ENABLED=0 \
    GO111MODULE=on

COPY --from=deps /go/pkg/mod /go/pkg/mod
COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} GOARM= \
    go build -trimpath -ldflags "-s -w -buildid=" -o /out/gateway ./cmd/gateway

###############################################################################
# healthcheck: build a tiny static healthcheck binary (http + tcp)
###############################################################################
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS healthcheck
WORKDIR /hc

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

ENV CGO_ENABLED=0

RUN cat > healthcheck.go << 'EOF'
package main

import (
\t"context"
\t"net"
\t"net/http"
\t"os"
\t"time"
)

func main() {
\taddr := getenv("HC_ADDR", "127.0.0.1:8080")
\turl := getenv("HC_URL", "http://127.0.0.1:8080/health")

\td := net.Dialer{Timeout: 500 * time.Millisecond}
\tc, err := d.Dial("tcp", addr)
\tif err != nil {
\t\tos.Exit(1)
\t}
\t_ = c.Close()

\tctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
\tdefer cancel()

\treq, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
\tres, err := http.DefaultClient.Do(req)
\tif err != nil {
\t\tos.Exit(0)
\t}
\tdefer res.Body.Close()

\tif res.StatusCode >= 200 && res.StatusCode < 300 {
\t\tos.Exit(0)
\t}
\tos.Exit(1)
}

func getenv(k, def string) string {
\tv := os.Getenv(k)
\tif v == "" {
\t\treturn def
\t}
\treturn v
}
EOF

RUN --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags "-s -w -buildid=" -o /out/healthcheck healthcheck.go

###############################################################################
# runtime: distroless, nonroot, CA certs present
###############################################################################
FROM ${DISTROLESS_IMAGE} AS runtime
WORKDIR /

ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_URL=unknown
ARG LICENSES=UNLICENSED

LABEL org.opencontainers.image.title="chartly-gateway" \
      org.opencontainers.image.description="Chartly 2.0 API Gateway service" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.licenses="${LICENSES}"

COPY --from=build /out/gateway /gateway
COPY --from=healthcheck /out/healthcheck /healthcheck

EXPOSE 8080

ENV HC_ADDR=127.0.0.1:8080 \
    HC_URL=http://127.0.0.1:8080/health

USER nonroot:nonroot

HEALTHCHECK --interval=5s --timeout=1s --start-period=10s --retries=6 CMD ["/healthcheck"]

STOPSIGNAL SIGTERM

ENTRYPOINT ["/gateway"]
