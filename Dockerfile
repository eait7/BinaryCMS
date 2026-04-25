# ── Stage 1: Build the Go binary ──
FROM golang:alpine AS builder
WORKDIR /app

# Install build dependencies completely automatically
RUN apk --no-cache add build-base sqlite-dev

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build securely
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w -X 'github.com/ez8/gocms/internal/handlers.GitCommit=docker-deploy' -X 'github.com/ez8/gocms/internal/handlers.BuildTime=$(date -u +'%Y-%m-%dT%H:%M:%SZ')'" -o gocms_server ./cmd/server

# ── Stage 2: Minimal runtime ──
FROM alpine:3.19
WORKDIR /app

# Ensure SQLite runtime binaries exist
RUN apk --no-cache add ca-certificates sqlite-libs

# Extract compiled server wrapper
COPY --from=builder /app/gocms_server /app/gocms_server
COPY --from=builder /app/themes /app/themes
COPY --from=builder /app/static /app/static

# Construct mapped persistence layers cleanly
RUN mkdir -p /app/uploads /app/data /app/plugins_data

EXPOSE 8080
ENTRYPOINT ["/app/gocms_server"]
