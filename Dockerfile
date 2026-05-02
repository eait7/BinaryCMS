# ── Stage 1: Build the Go binary ──
FROM golang:bookworm AS builder
WORKDIR /app

# Install build dependencies
RUN apt-get update && apt-get install -y build-essential libsqlite3-dev

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build securely
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w -X 'github.com/ez8/gocms/internal/handlers.GitCommit=docker-deploy' -X 'github.com/ez8/gocms/internal/handlers.BuildTime=$(date -u +'%Y-%m-%dT%H:%M:%SZ')'" -o gocms_server ./cmd/server

# ── Stage 2: Minimal runtime ──
FROM debian:bookworm-slim
WORKDIR /app

# Ensure SQLite runtime binaries exist
RUN apt-get update && apt-get install -y ca-certificates libsqlite3-0 gosu && rm -rf /var/lib/apt/lists/*

# Create restricted system user
RUN groupadd -r gocms && useradd --no-log-init -r -g gocms gocms

# Extract compiled server and assets
COPY --from=builder /app/gocms_server /app/gocms_server
COPY --from=builder /app/themes /app/themes
COPY --from=builder /app/static /app/static
COPY --from=builder /app/entrypoint.sh /app/entrypoint.sh

# Construct mapped persistence layers and set full ownership to gocms
# gocms must own /app so the self-updater can replace the binary
RUN mkdir -p /app/uploads /app/data /app/plugins /app/plugins_data \
    && chown -R gocms:gocms /app \
    && chmod +x /app/entrypoint.sh /app/gocms_server

EXPOSE 8080
ENTRYPOINT ["/app/entrypoint.sh"]
