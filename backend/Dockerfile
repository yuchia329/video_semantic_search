# Build stage
FROM golang:1-bookworm AS builder

WORKDIR /app

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy application source
COPY . .

# Build static binary
# (CGO_ENABLED=0 ensures a static binary, which is safe to run in alpine/slim images)
RUN CGO_ENABLED=0 go build -o /app/server .

# Runtime stage
# We use debian:bookworm-slim instead of alpine/scratch because the Go backend
# requires ffmpeg and python3 (for yt-dlp) to process videos in the worker.
FROM debian:bookworm-slim

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    python3 \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install yt-dlp
RUN curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp \
    && chmod a+rx /usr/local/bin/yt-dlp

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/server /app/server

EXPOSE 8080

CMD ["/app/server"]
