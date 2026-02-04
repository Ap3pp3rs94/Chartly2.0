FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/control-plane/coordinator ./services/control-plane/coordinator
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/coordinator ./services/control-plane/coordinator

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/coordinator /app/coordinator
EXPOSE 8083
CMD ["/app/coordinator"]

