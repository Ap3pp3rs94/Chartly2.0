FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/codex-executor ./services/codex-executor
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/codex-executor ./services/codex-executor/cmd/codex-executor

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/codex-executor /app/codex-executor
EXPOSE 8087
CMD ["/app/codex-executor"]
