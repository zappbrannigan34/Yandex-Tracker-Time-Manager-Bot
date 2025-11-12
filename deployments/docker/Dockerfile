# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o time-tracker-bot ./cmd/time-tracker-bot

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates curl bash

# Install yc CLI
RUN curl -sSL https://storage.yandexcloud.net/yandexcloud-yc/install.sh | bash && \
    mv /root/yandex-cloud/bin/yc /usr/local/bin/yc

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/time-tracker-bot .

# Copy config example and data files
COPY config.yaml.example config.yaml
COPY data/ data/

# Create directories
RUN mkdir -p state logs

# Create non-root user
RUN addgroup -S tracker && adduser -S tracker -G tracker && \
    chown -R tracker:tracker /app

USER tracker

# Default command
CMD ["./time-tracker-bot", "daemon"]
