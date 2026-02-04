FROM golang:1.21-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/control-plane/reporter ./services/control-plane/reporter
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/reporter ./services/control-plane/reporter

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/reporter /app/reporter
EXPOSE 8084
CMD ["/app/reporter"]
