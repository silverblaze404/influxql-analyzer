# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code (excluding scripts folder)
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY filtering_rules.yaml .

# Build the application
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o influxql-analyzer cmd/influxql-analyzer/main.go

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/influxql-analyzer .
COPY --from=builder /app/filtering_rules.yaml .

# Create directories for input/output/config
RUN mkdir -p /app/logs /app/output /app/config

# Set the entrypoint
ENTRYPOINT ["/app/influxql-analyzer"]

# Default command shows help
CMD ["--help"]

# Usage examples:
# Build the image:
#   docker build -t influxql-analyzer .
#
# Show help:
#   docker run --rm influxql-analyzer
#
# Analyze a log file (mount directory):
#   docker run --rm -v /path/to/your/logs:/app/logs -v /path/to/output:/app/output influxql-analyzer -log /app/logs/access.log -output /app/output/results.json
#
# Analyze a log file (mount specific file):
#   docker run --rm -v /var/log/influxdb/access.log:/app/logs/access.log:ro -v /tmp/analysis:/app/output influxql-analyzer -log /app/logs/access.log -output /app/output/results.json
#
# Use custom config:
#   docker run --rm -v /path/to/your/logs:/app/logs -v /path/to/output:/app/output -v /path/to/config:/app/config influxql-analyzer -log /app/logs/access.log -output /app/output/results.json -config /app/config/my_rules.yaml
#
# Mount specific files (more secure):
#   docker run --rm -v /var/log/influxdb/access.log:/app/logs/access.log:ro -v /home/user/my_rules.yaml:/app/config/my_rules.yaml:ro -v /tmp/analysis:/app/output influxql-analyzer -log /app/logs/access.log -output /app/output/results.json -config /app/config/my_rules.yaml
