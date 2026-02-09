# Chartly 2.0  Audit Service Dockerfile (multi-stage, production-grade)
#
# Build args:
#   BUILD_VERSION - semantic or build version string
#   BUILD_COMMIT  - git commit sha
#   BUILD_DATE    - RFC3339 build timestamp
#
# Notes:
# - Uses Go modules and a dedicated build stage for reproducible caching.
# - Produces a minimal runtime image and runs as non-root.

# -------------------------
# Build stage
# -------------------------
FROM golang:1.25-alpine AS build

WORKDIR /src

# Install build tooling (git for module fetch, ca-certs for HTTPS)
RUN apk add --no-cache ca-certificates git

# Cache deps first
COPY go.mod go.sum ./
RUN go mod download

# Copy service source
COPY services/audit ./services/audit

# Build args for ldflags (safe defaults)
ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=unknown
ARG BUILD_DATE=unknown

# Build a mostly-static binary
# NOTE: If cmd/audit/main.go does not define these vars, ldflags will be ignored by the linker.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w \
      -X main.buildVersion=${BUILD_VERSION} \
      -X main.buildCommit=${BUILD_COMMIT} \
      -X main.buildDate=${BUILD_DATE}" \
    -o /out/audit ./services/audit/cmd/audit

# -------------------------
# Runtime stage
# -------------------------
FROM gcr.io/distroless/static:nonroot AS runtime

WORKDIR /

# Copy binary
COPY --from=build /out/audit /audit

# Service default port
EXPOSE 8084

# Environment defaults (service reads env; keep minimal)
ENV AUDIT_ADDR=0.0.0.0
ENV AUDIT_PORT=8084

USER nonroot:nonroot

ENTRYPOINT ["/audit"]
