# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.23
ARG DISTROLESS_IMAGE=gcr.io/distroless/base-debian12:nonroot

###############################################################################
# deps: download modules with caching
###############################################################################
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS deps
WORKDIR /src

COPY go.mod go.sum ./
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
# build: compile orchestrator
###############################################################################
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build
WORKDIR /src

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

# Build metadata (injected by CI)
ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_URL=unknown
ARG LICENSES=UNLICENSED

ENV CGO_ENABLED=0 \
    GO111MODULE=on

COPY --from=deps /go/pkg/mod /go/pkg/mod
COPY . .

# Build orchestrator with reproducible flags
RUN --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} GOARM= \
    go build -trimpath \
      -ldflags "-s -w \
        -buildid= \
        -X main.version=${VERSION} \
        -X main.commit=${VCS_REF} \
        -X main.date=${BUILD_DATE}" \
      -o /out/orchestrator .

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
	"context"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := getenv("HC_ADDR", "127.0.0.1:8081")
	url := getenv("HC_URL", "http://127.0.0.1:8081/health")

	// TCP probe
	d := net.Dialer{Timeout: 500 * time.Millisecond}
	c, err := d.Dial("tcp", addr)
	if err != nil {
		os.Exit(1)
	}
	_ = c.Close()

	// HTTP probe (best-effort: only fail hard if endpoint responds non-2xx)
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		// If TCP is up but HTTP isn't ready yet, allow during warmup.
		os.Exit(0)
	}
	defer res.Body.Close()

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		os.Exit(0)
	}

	os.Exit(1)
}

func getenv(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
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

LABEL org.opencontainers.image.title="chartly-orchestrator" \
      org.opencontainers.image.description="Chartly 2.0 Orchestrator service" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.licenses="${LICENSES}"

COPY --from=build /out/orchestrator /orchestrator
COPY --from=healthcheck /out/healthcheck /healthcheck

EXPOSE 8081

# Healthcheck env defaults (override at runtime if needed)
ENV HC_ADDR=127.0.0.1:8081 \
    HC_URL=http://127.0.0.1:8081/health

# Distroless nonroot
USER nonroot:nonroot

# Fast & strict container health signal without curl
HEALTHCHECK --interval=5s --timeout=1s --start-period=10s --retries=6 CMD ["/healthcheck"]

# Prefer SIGTERM for graceful shutdown
STOPSIGNAL SIGTERM

ENTRYPOINT ["/orchestrator"]
