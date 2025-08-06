# InfluxQL Analyzer

🔍 **Analyze InfluxDB v1 HTTP access logs to identify problematic queries that impact database performance**

This Docker image contains a powerful tool for analyzing InfluxDB v1 access logs to detect queries that may cause performance issues. It validates queries against configurable filtering rules and generates detailed JSON reports.

## 🚀 Quick Start

**Show help:**

```bash
docker run --rm ranjan97/influxql-analyzer
```

**Analyze a log file:**

```bash
docker run --rm \
  -v /var/log/influxdb/access.log:/app/logs/access.log:ro \
  -v /tmp/analysis:/app/output \
  ranjan97/influxql-analyzer \
  -log /app/logs/access.log \
  -output /app/output/results.json
```

## 📋 What It Detects

- ⏰ **Time range violations**: Queries exceeding configured time limits or missing time filters
- 🔍 **Regex usage**: Queries using regex operators (=~ or !~) with optional blocking
- 📊 **High OFFSET values**: Pagination queries with large OFFSET parameters
- 🚫 **Blocked functions and statements**: DELETE, DROP, or other restricted operations
- 🔍 **Wildcard SELECT without LIMIT**: SELECT * queries without proper limits
- 📈 **Unlimited GROUP BY queries**: Aggregations without time bucketing or limits
- ⚡ **Expensive SHOW commands**: SHOW SERIES without appropriate constraints
- 🎯 **Measurement restrictions**: Queries on unauthorized measurements (if enabled)
- ⏱️ **Long-running queries**: Queries exceeding duration warning thresholds

## 🛠️ Usage Examples

### Basic Analysis

```bash
# Analyze access logs with default configuration
docker run --rm \
  -v /var/log/influxdb:/app/logs \
  -v /tmp/results:/app/output \
  ranjan97/influxql-analyzer \
  -log /app/logs/access.log \
  -output /app/output/analysis.json
```

### Secure File Mounting (Recommended)

```bash
# Mount only specific files with read-only access
docker run --rm \
  -v /var/log/influxdb/access.log:/app/logs/access.log:ro \
  -v /tmp/results:/app/output \
  ranjan97/influxql-analyzer \
  -log /app/logs/access.log \
  -output /app/output/analysis.json
```

### Custom Configuration

```bash
# Use your own filtering rules
docker run --rm \
  -v /var/log/influxdb/access.log:/app/logs/access.log:ro \
  -v /path/to/my_rules.yaml:/app/config/my_rules.yaml:ro \
  -v /tmp/results:/app/output \
  ranjan97/influxql-analyzer \
  -log /app/logs/access.log \
  -config /app/config/my_rules.yaml \
  -output /app/output/analysis.json
```

## 📁 Volume Mounts

| Container Path | Purpose | Required |
|----------------|---------|----------|
| `/app/logs` | InfluxDB log files | ✅ Yes |
| `/app/output` | Analysis results | ✅ Yes |
| `/app/config` | Custom configuration | ❌ Optional |

## ⚙️ Configuration

The tool uses a YAML configuration file to define filtering rules:

- **Time filtering**: Require time filters, max time ranges, query duration warnings
- **Regex usage control**: Warn on or block regex operators (=~ or !~)
- **Performance limits**: Query duration warnings, LIMIT requirements, OFFSET limits
- **Security rules**: Block dangerous functions/statements
- **Measurement filtering**: Restrict allowed measurements

**Default configuration** is included in the image at `/app/filtering_rules.yaml`. To view the default settings:

```bash
docker run --rm ranjan97/influxql-analyzer cat /app/filtering_rules.yaml
```

## 📊 Output Format

Generates JSON reports with:

- **Analysis Summary**: Total problematic queries found
- **Detailed Results**: Each issue with timestamps, reasons, and log context
- **Query Information**: Extracted queries with metadata

## 🔒 Security Best Practices

1. **Use read-only mounts**: Add `:ro` to prevent accidental writes
2. **Mount specific files**: Only mount files you need to analyze
3. **Separate output directory**: Use dedicated directory for results

## 🐳 Image Details

- **Base**: Alpine Linux (minimal footprint)
- **Size**: ~15MB compressed
- **Architecture**: linux/amd64
- **Go Version**: 1.23+

## 🔗 Links

- **GitHub Repository**: [influxql-analyzer](https://github.com/silverblaze404/influxql-analyzer)
- **Documentation**: Full README with examples
- **Issues**: Report bugs or feature requests

## 📖 Example Output

```json
{
  "analysis_summary": {
    "total_problematic_queries": 52,
    "analysis_timestamp": "2025-08-06T04:22:09Z"
  },
  "problematic_queries": [
    {
      "LogEntry": {
        "RawLine": "10.11.8.19 - root [05/Aug/2025:09:20:12 +0000] \"GET /query?chunk_size=10000&chunked=true&db=Alteryx&q=select+cron_ran_at+from+rp_seven_days.grid_barcode_mapping+where+time+%3C+%272025-08-05T04%3A48%3A00Z%27+and+time+%3Enow%28%29-30d+order+by+time+desc+limit+1 HTTP/1.1\" 200 160"
      },
      "HasTimeFilter": false,
      "TimeRange": null,
      "Issue": "BLOCKED_BY_FILTER",
      "Reason": "Time range (696h25m50s) exceeds maximum allowed (480h0m0s)"
    },
    {
      "LogEntry": {
        "RawLine": "10.11.8.19 - root [05/Aug/2025:09:21:06 +0000] \"GET /query?chunk_size=10000&chunked=true&db=GreyOrange&q=select+unique_id%2Cfirst_put_time%2Cfirst_put_uom+from+rp_seven_days.slots_ageing_elk+where+time+%3E%3D+%272025-08-05+02%3A25%3A17%27+and+time%3C%3D+%272025-08-05T09%3A21%3A00Z%27+and+remaining_uom%21%3D%270%27+order+by+time+desc+limit+5000+offset+15000 HTTP/1.1\" 200 72064"
      },
      "HasTimeFilter": false,
      "TimeRange": null,
      "Issue": "BLOCKED_BY_FILTER",
      "Reason": "OFFSET value (15000) exceeds maximum allowed (10000)"
    }
  ]
}
```

## 💡 Use Cases

- **Database Performance Monitoring**: Identify slow queries
- **Security Auditing**: Detect potentially dangerous queries  
- **Capacity Planning**: Understand query patterns
- **Troubleshooting**: Root cause analysis for performance issues
