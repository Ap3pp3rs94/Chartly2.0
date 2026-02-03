FROM golang:1.21-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/control-plane/registry ./services/control-plane/registry
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/registry ./services/control-plane/registry

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/registry /app/registry
COPY profiles /app/profiles
EXPOSE 8081
CMD ["/app/registry"]
