# influxql-analyzer

Influxdb v1 http access logging analysis

## Overview

This tool analyzes InfluxDB v1 HTTP access logs to identify problematic queries that may impact database performance. It validates queries against configurable filtering rules and generates detailed reports of issues found.

## Local Development

### Prerequisites

- Go 1.24 or later
- Git

### Clone, Build, and Run

1. **Clone the repository:**

   ```bash
   git clone https://github.com/silverblaze404/influxql-analyzer.git
   cd influxql-analyzer
   ```

2. **Download dependencies:**

   ```bash
   go mod download
   ```

3. **Build the application:**

   ```bash
   go build -o influxql-analyzer cmd/influxql-analyzer/main.go
   ```

4. **Run the application:**

   ```bash
   # Show help
   ./influxql-analyzer --help
   
   # Analyze a log file
   ./influxql-analyzer -log /path/to/your/influxdb/access.log -output results.json
   
   # Use custom configuration
   ./influxql-analyzer -log /path/to/your/influxdb/access.log -output results.json -config my_rules.yaml
   ```

### Running Tests

Run the test suite to ensure everything is working correctly:

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for a specific package
go test ./internal/filter
```

## Docker Usage

### Option 1: Use Pre-built Image (Recommended)

The easiest way to use influxql-analyzer is with the pre-built Docker image:

```bash
# No need to build - just run directly!
docker run --rm ranjan97/influxql-analyzer
```

### Option 2: Build Your Own Image

If you prefer to build the image yourself:

```bash
docker build -t influxql-analyzer .
```

### Running with Docker

1. **Show help:**

   ```bash
   # Using pre-built image
   docker run --rm ranjan97/influxql-analyzer
   
   # Or using your own build
   docker run --rm influxql-analyzer
   ```

2. **Analyze a log file (mount directory):**

   ```bash
   docker run --rm \
     -v /path/to/your/logs:/app/logs \
     -v /path/to/output:/app/output \
     ranjan97/influxql-analyzer \
     -log /app/logs/access.log \
     -output /app/output/results.json
   ```

3. **Analyze a log file (mount specific file - more secure):**

   ```bash
   docker run --rm \
     -v /var/log/influxdb/access.log:/app/logs/access.log:ro \
     -v /tmp/analysis:/app/output \
     ranjan97/influxql-analyzer \
     -log /app/logs/access.log \
     -output /app/output/results.json
   ```

4. **Use custom configuration:**

   ```bash
   docker run --rm \
     -v /path/to/your/logs:/app/logs \
     -v /path/to/output:/app/output \
     -v /path/to/your/config:/app/config \
     ranjan97/influxql-analyzer \
     -log /app/logs/access.log \
     -output /app/output/results.json \
     -config /app/config/my_rules.yaml
   ```

5. **Mount specific files (most secure approach):**

   ```bash
   docker run --rm \
     -v /var/log/influxdb/access.log:/app/logs/access.log:ro \
     -v /home/user/my_rules.yaml:/app/config/my_rules.yaml:ro \
     -v /tmp/analysis:/app/output \
     ranjan97/influxql-analyzer \
     -log /app/logs/access.log \
     -output /app/output/results.json \
     -config /app/config/my_rules.yaml
   ```

### Docker Volume Mounts Explained

- `/app/logs` - Mount your directory containing InfluxDB log files
- `/app/output` - Mount the directory where you want analysis results saved
- `/app/config` - (Optional) Mount directory containing custom configuration files

### Mounting Options

**Directory Mounting (easier):**

```bash
-v /var/log/influxdb:/app/logs
```

- Mounts entire directory
- Container can access all files in the directory
- Good for multiple log files

**File Mounting (more secure):**

```bash
-v /var/log/influxdb/access.log:/app/logs/access.log:ro
```

- Mounts only specific file
- `:ro` makes it read-only for extra security
- Container only sees the specific file you allow
- Recommended for production use

### Example with Real Paths

```bash
# If your InfluxDB logs are in /var/log/influxdb/ and you want output in /tmp/analysis/
docker run --rm \
  -v /var/log/influxdb:/app/logs \
  -v /tmp/analysis:/app/output \
  ranjan97/influxql-analyzer \
  -log /app/logs/access.log \
  -output /app/output/influx_analysis.json
```

## Configuration

The application uses a YAML configuration file (`filtering_rules.yaml`) to define query validation rules. You can customize:

- Time range requirements and limits
- Query duration warnings
- Blocked functions and statements
- Allowed measurements
- Performance filtering options

### Default Configuration

The default configuration file is included in the repository as `filtering_rules.yaml`.

**For Docker users**, you can view the default configuration:

```bash
docker run --rm ranjan97/influxql-analyzer cat /app/filtering_rules.yaml
```

## Output

The analyzer generates a JSON report containing:

- Analysis summary with total problematic queries found
- Detailed list of each problematic query with:
  - Original log entry information
  - Issue type and reason
  - Time filter analysis
  - Timestamp and client information
