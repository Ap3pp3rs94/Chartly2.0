FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/topics ./services/topics
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/topics ./services/topics/cmd/topics

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/topics /app/topics
COPY services/topics/config /app/config
COPY profiles/topics /app/profiles/topics
EXPOSE 8087
CMD ["/app/topics","--config","/app/config/topics.yaml"]
