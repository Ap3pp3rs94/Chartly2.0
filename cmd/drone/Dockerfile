FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
WORKDIR /build/cmd/drone
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /drone .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /drone /app/drone
ENTRYPOINT ["/app/drone"]
