FROM golang:1.21-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/control-plane/gateway ./services/control-plane/gateway
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/gateway ./services/control-plane/gateway

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/gateway /app/gateway
COPY web/dist /app/web/dist
EXPOSE 8080
CMD ["/app/gateway"]
