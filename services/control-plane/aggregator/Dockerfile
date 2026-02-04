FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/control-plane/aggregator ./services/control-plane/aggregator
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /app/aggregator ./services/control-plane/aggregator

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
RUN mkdir -p /app/data
COPY --from=builder /app/aggregator /app/aggregator
EXPOSE 8082
CMD ["/app/aggregator"]

