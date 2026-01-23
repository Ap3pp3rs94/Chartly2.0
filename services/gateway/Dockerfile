# syntax=docker/dockerfile:1.7

# Chartly 2.0  Gateway service (Go)
# Multi-stage build: compile in golang, run in distroless.

ARG GO_VERSION=1.22

FROM golang:${GO_VERSION} AS builder
WORKDIR /src

# Copy workspace/module metadata first for better caching
COPY ../../go.work ../../go.mod ./

# Copy service source
COPY . ./services/gateway

WORKDIR /src/services/gateway

# Build
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.source="https://github.com/Ap3pp3rs94/Chartly2.0"
LABEL org.opencontainers.image.title="chartly-gateway"
LABEL org.opencontainers.image.description="Chartly 2.0 API Gateway service"

EXPOSE 8080

COPY --from=builder /out/gateway /gateway

ENTRYPOINT ["/gateway"]
