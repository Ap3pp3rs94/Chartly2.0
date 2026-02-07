# Chartly 2.0  Auth Service Dockerfile (multi-stage, production-grade)
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
FROM golang:1.22-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY services/auth ./services/auth

ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w \
      -X main.buildVersion=${BUILD_VERSION} \
      -X main.buildCommit=${BUILD_COMMIT} \
      -X main.buildDate=${BUILD_DATE}" \
    -o /out/auth ./services/auth/cmd/auth

# -------------------------
# Runtime stage
# -------------------------
FROM gcr.io/distroless/static:nonroot AS runtime

WORKDIR /
COPY --from=build /out/auth /auth

EXPOSE 8085

ENV AUTH_ADDR=0.0.0.0
ENV AUTH_PORT=8085

USER nonroot:nonroot

ENTRYPOINT ["/auth"]
