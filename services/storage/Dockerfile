# syntax=docker/dockerfile:1.6

ARG GO_VERSION=1.22
ARG DISTROLESS_IMAGE=gcr.io/distroless/static-debian12:nonroot

############################################
# deps
############################################
FROM golang:${GO_VERSION}-bookworm AS deps
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download

############################################
# build
############################################
FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src

ARG VERSION="0.0.0"
ARG COMMIT="dev"

ENV CGO_ENABLED=0 GO111MODULE=on

COPY --from=deps /go/pkg/mod /go/pkg/mod
COPY . .

# Build deterministic, static linux/amd64 binary
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    GOOS=linux GOARCH=amd64 \
    go build -trimpath -buildvcs=false \
      -ldflags "-s -w -buildid= -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/storage ./services/storage/cmd/storage

############################################
# healthcheck
############################################
FROM golang:${GO_VERSION}-bookworm AS healthcheck
WORKDIR /hc

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
	addr := getenv("HC_ADDR", "127.0.0.1:8083")
	url := getenv("HC_URL", "http://127.0.0.1:8083/health")

	d := net.Dialer{Timeout: 500 * time.Millisecond}
	c, err := d.Dial("tcp", addr)
	if err != nil {
		os.Exit(1)
	}
	_ = c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		os.Exit(1)
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
    GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w -buildid=" -o /out/healthcheck healthcheck.go

############################################
# runtime
############################################
FROM ${DISTROLESS_IMAGE} AS runtime
WORKDIR /

# metadata args
ARG VERSION="0.0.0"
ARG COMMIT="dev"

LABEL org.opencontainers.image.title="chartly-storage" \
      org.opencontainers.image.description="Chartly 2.0 Storage service" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}"

COPY --from=build /out/storage /storage
COPY --from=healthcheck /out/healthcheck /healthcheck

EXPOSE 8083

ENV HC_ADDR=127.0.0.1:8083 \
    HC_URL=http://127.0.0.1:8083/health

USER nonroot:nonroot

HEALTHCHECK --interval=10s --timeout=2s --retries=12 --start-period=5s CMD ["/healthcheck"]

ENTRYPOINT ["/storage"]
