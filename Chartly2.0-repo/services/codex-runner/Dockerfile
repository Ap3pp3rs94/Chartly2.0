FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/codex-runner ./services/codex-runner
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/codex-runner ./services/codex-runner/cmd/codex-runner

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/codex-runner /app/codex-runner
COPY profiles /app/profiles
EXPOSE 8086
CMD ["/app/codex-runner"]
