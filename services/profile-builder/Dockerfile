FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/profile-builder ./services/profile-builder
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/profile-builder ./services/profile-builder/cmd/profile-builder

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/profile-builder /app/profile-builder
EXPOSE 8085
CMD ["/app/profile-builder"]
