FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY services/analytics/go.mod ./
RUN go mod download
COPY services/analytics ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/analytics ./cmd/analytics

FROM alpine:latest
RUN apk --no-cache add ca-certificates wget
WORKDIR /app
COPY --from=builder /app/analytics /app/analytics
EXPOSE 8086
ENV ANALYTICS_ADDR=:8086
HEALTHCHECK --interval=5s --timeout=1s --start-period=10s --retries=6 CMD wget -q --spider http://127.0.0.1:8086/health || exit 1
CMD ["/app/analytics"]
