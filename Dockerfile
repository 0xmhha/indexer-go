# Build stage
FROM golang:1.26.8-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build binary with version information. CHAIN selects the binary: empty
# builds indexer (generic EVM), a chain under x/ builds indexer-<chain>. The
# image always runs it as /app/indexer; the binary reports its own name.
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown
ARG CHAIN=

RUN main=./cmd/indexer; \
    if [ -n "${CHAIN}" ]; then main=./x/${CHAIN}/cmd/indexer-${CHAIN}; fi; \
    go build -ldflags "-s -w \
    -X github.com/0xmhha/indexer-go/cmd/cli.version=${VERSION} \
    -X github.com/0xmhha/indexer-go/cmd/cli.commit=${COMMIT} \
    -X github.com/0xmhha/indexer-go/cmd/cli.buildTime=${BUILD_TIME}" \
    -o indexer "$main"

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata wget

# Create app user
RUN addgroup -g 1000 indexer && \
    adduser -D -u 1000 -G indexer indexer

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/indexer /app/indexer

# Create data directory
RUN mkdir -p /data && chown -R indexer:indexer /data /app

# Switch to app user
USER indexer

# Expose API port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/health || exit 1

# Run indexer
ENTRYPOINT ["/app/indexer"]
CMD ["--rpc", "http://localhost:8545", "--db", "/data", "--api", "--graphql", "--jsonrpc", "--websocket"]
