# ============================================================
# Stage 1: Build the picoclaw binary
# ============================================================
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 go build -tags "goolm,stdjson" -ldflags="-s -w" -o build/picoclaw ./cmd/picoclaw

# ============================================================
# Stage 2: Minimal runtime image
# ============================================================
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata curl wget

# Copy binary and entrypoint
COPY --from=builder /src/build/picoclaw /usr/local/bin/picoclaw
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Copy builtin skills into runtime image
COPY skills /skills


# Default port for local testing; Render injects its own $PORT dynamically
ENV PORT=18790
EXPOSE 18790

# Health check against PicoClaw HTTP gateway /health
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q --spider http://127.0.0.1:${PORT}/health || exit 1

ENTRYPOINT ["/entrypoint.sh"]
