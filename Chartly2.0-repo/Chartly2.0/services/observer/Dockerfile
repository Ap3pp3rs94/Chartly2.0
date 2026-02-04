# syntax=docker/dockerfile:1.7
# Chartly 2.0 - Observer Service (production)
#
# Build args:
#   BUILD_VERSION - semantic/build version string
#   BUILD_COMMIT  - git commit SHA
#   BUILD_DATE    - RFC3339 build timestamp

# -------------------------
# Build stage
# -------------------------
FROM golang:1.22-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates git

# Copy module definition first for better layer caching
COPY go.mod ./

# If there are no local replaces, prefetch deps now
RUN set -eux; \
    if ! grep -E '=>\s*(\./|\../)' go.mod; then \
        go mod download; \
    else \
        echo "local replace detected; delaying go mod download"; \
    fi

# Copy the rest of the service source
COPY . .

ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=unknown
ARG BUILD_DATE=unknown

# Build a mostly-static binary with optional build metadata
RUN set -eux; \
    LDFLAGS="-s -w"; \
    if [ -f cmd/observer/main.go ]; then \
        if grep -q "buildVersion" cmd/observer/main.go; then LDFLAGS="$LDFLAGS -X main.buildVersion=${BUILD_VERSION}"; fi; \
        if grep -q "buildCommit" cmd/observer/main.go; then LDFLAGS="$LDFLAGS -X main.buildCommit=${BUILD_COMMIT}"; fi; \
        if grep -q "buildDate" cmd/observer/main.go; then LDFLAGS="$LDFLAGS -X main.buildDate=${BUILD_DATE}"; fi; \
    fi; \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o /out/observer ./cmd/observer

# -------------------------
# Runtime stage
# -------------------------
FROM gcr.io/distroless/static:nonroot AS runtime

WORKDIR /

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/observer /observer

EXPOSE 8086

ENV OBSERVER_ADDR=0.0.0.0
ENV OBSERVER_PORT=8086

USER nonroot:nonroot

ENTRYPOINT ["/observer"]
