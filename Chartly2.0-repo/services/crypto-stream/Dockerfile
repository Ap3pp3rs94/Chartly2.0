FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/crypto-stream ./services/crypto-stream
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/crypto-stream ./services/crypto-stream

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/crypto-stream /app/crypto-stream
EXPOSE 8088
CMD ["/app/crypto-stream"]
