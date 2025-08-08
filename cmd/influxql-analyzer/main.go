package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"influxql-analyzer/internal/config"
	"influxql-analyzer/internal/filter"

	"github.com/sirupsen/logrus"
)

var (
	configFile = flag.String("config", "filtering_rules.yaml", "Path to configuration file")
)

// LogEntry represents a parsed log line
type LogEntry struct {
	Timestamp    string
	Level        string
	Method       string
	Path         string
	Query        string
	StatusCode   int
	ResponseTime string
	ClientIP     string
	RawLine      string
}

// AnalysisResult represents the analysis result for a query
type AnalysisResult struct {
	LogEntry      LogEntry
	HasTimeFilter bool
	TimeRange     *filter.TimeRange
	Issue         string
	Reason        string
}

// QueryAnalyzer analyzes queries for issues using the filter rules
type QueryAnalyzer struct {
	queryFilter *filter.QueryFilter
	config      *config.FilteringRules
}

func main() {
	var (
		logFilePath    = flag.String("log", "", "Path to the InfluxDB access log file")
		outputFilePath = flag.String("output", "problematic_queries.json", "Path to the output file")
		help           = flag.Bool("help", false, "Show help message")
	)
	flag.Parse()

	if *help || *logFilePath == "" {
		printUsage()
		return
	}

	analyzer := NewQueryAnalyzer()

	fmt.Printf("Analyzing log file: %s\n", *logFilePath)
	fmt.Printf("Maximum allowed time range: %d hours (%.1f days)\n", analyzer.config.MaxTimeRangeHours, float64(analyzer.config.MaxTimeRangeHours)/24.0)
	fmt.Printf("Output file: %s\n", *outputFilePath)

	results, err := analyzer.AnalyzeLogFile(*logFilePath)
	if err != nil {
		log.Fatalf("Error analyzing log file: %v", err)
	}

	if len(results) == 0 {
		fmt.Println("No problematic queries found!")
		return
	}

	err = writeResults(results, *outputFilePath)
	if err != nil {
		log.Fatalf("Error writing results: %v", err)
	}

	fmt.Printf("Found %d problematic queries. Results written to %s\n", len(results), *outputFilePath)
	printSummary(results)
}

func printUsage() {
	fmt.Println("InfluxDB Log Analyzer")
	fmt.Println("=====================")
	fmt.Println("Analyzes InfluxDB access logs to find queries with missing or excessive time filters.")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  log-analyzer -log <path> [options]")
	fmt.Println()
	fmt.Println("Required:")
	fmt.Println("  -log string")
	fmt.Println("        Path to the InfluxDB access log file")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -config string")
	fmt.Println("        Path to configuration file (default: filtering_rules.yaml)")
	fmt.Println("  -output string")
	fmt.Println("        Path to the output file (default: problematic_queries.json)")
	fmt.Println("  -help")
	fmt.Println("        Show this help message")
	fmt.Println()
	fmt.Println("Note:")
	fmt.Println("  Maximum time range is configured in the config file via 'max_time_range_hours'")
	fmt.Println("  Log level and format can be configured in the config file via 'logging.level' and 'logging.format'")
	fmt.Println("  Supported log levels: trace, debug, info, warn, error, fatal, panic")
	fmt.Println("  Supported log formats: text, json")
	fmt.Println()
	fmt.Println("Example:")
	fmt.Println("  log-analyzer -log /var/log/influxdb/access.log -output results.json -config my_rules.yaml")
}

func configureLogger(cfg *config.FilteringRules) {
	level, err := logrus.ParseLevel(strings.ToLower(cfg.Logging.Level))
	if err != nil {
		log.Printf("Warning: Invalid log level '%s', using 'info' instead", cfg.Logging.Level)
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)

	// Set log format
	switch strings.ToLower(cfg.Logging.Format) {
	case "json":
		logrus.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339,
		})
	case "text":
		logrus.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339,
		})
	default:
		log.Printf("Warning: Invalid log format '%s', using 'text' instead", cfg.Logging.Format)
		logrus.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339,
		})
	}
}

func NewQueryAnalyzer() *QueryAnalyzer {
	// Load configuration
	cfg, err := config.Load(*configFile)
	if err != nil {
		// If config file doesn't exist, create a default configuration
		if os.IsNotExist(err) {
			fmt.Printf("Configuration file '%s' not found, using default settings\n", *configFile)
			cfg = &config.FilteringRules{
				RequireTimeFilter:      true,
				MaxTimeRangeHours:      840, // 35 days
				WarnQueryDurationHours: 336, // 14 days
				MaxShowSeriesLimit:     10000,
				Logging: config.LoggingConfig{
					Level:  "info",
					Format: "json",
				},
			}
		} else {
			log.Fatalf("Failed to load configuration: %v", err)
		}
	}

	// Configure logger based on configuration
	configureLogger(cfg)

	return &QueryAnalyzer{
		queryFilter: filter.NewQueryFilter(*cfg),
		config:      cfg,
	}
}

func (qa *QueryAnalyzer) AnalyzeLogFile(filePath string) ([]AnalysisResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	var results []AnalysisResult
	scanner := bufio.NewScanner(file)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		logEntry, err := qa.parseLogLine(line)
		if err != nil {
			// Skip lines that don't contain queries or can't be parsed
			continue
		}

		if logEntry.Query == "" {
			continue
		}

		result := qa.analyzeQuery(logEntry)
		if result != nil {
			results = append(results, *result)
		}

		// Print progress every 1000 lines
		if lineNumber%1000 == 0 {
			fmt.Printf("Processed %d lines...\n", lineNumber)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading log file: %w", err)
	}

	fmt.Printf("Finished processing %d lines\n", lineNumber)
	return results, nil
}

func (qa *QueryAnalyzer) parseLogLine(line string) (LogEntry, error) {
	// This function attempts to parse different log formats
	// Common InfluxDB log formats include:
	// 1. Standard HTTP access logs (Common Log Format)
	// 2. JSON formatted logs
	// 3. Custom formatted logs

	entry := LogEntry{RawLine: line}

	// Only consider /query API calls - skip all other endpoints
	if !strings.Contains(line, "/query") {
		return entry, fmt.Errorf("skipping non-query API call")
	}

	// Try to extract query from URL parameters or request body
	query := qa.extractQueryFromLine(line)
	if query == "" {
		return entry, fmt.Errorf("no query found in line")
	}

	entry.Query = query

	// Extract other fields
	entry.Timestamp = qa.extractTimestamp(line)
	entry.Method = qa.extractMethod(line)
	entry.ClientIP = qa.extractClientIP(line)
	entry.StatusCode = qa.extractStatusCode(line)
	entry.ResponseTime = qa.extractResponseTime(line)

	return entry, nil
}

func (qa *QueryAnalyzer) extractQueryFromLine(line string) string {
	// Parse the HTTP request from the log line
	req, err := qa.parseHTTPRequestFromLogLine(line)
	if err != nil {
		return ""
	}

	// Use the same query extraction logic as the proxy server
	query, _, err := qa.extractQuery(req)
	if err != nil {
		return ""
	}

	return query
}

// parseHTTPRequestFromLogLine reconstructs an HTTP request from a log line
func (qa *QueryAnalyzer) parseHTTPRequestFromLogLine(line string) (*http.Request, error) {
	// Extract the HTTP method, path, and body from the log line
	// Pattern for Common Log Format with request body
	// Example: POST /query?db=test HTTP/1.1 {'q': 'SELECT * FROM measurement'}

	// First extract the quoted request part
	requestPattern := `"([^"]+)"`
	re := regexp.MustCompile(requestPattern)
	matches := re.FindAllStringSubmatch(line, -1)

	if len(matches) < 1 {
		return nil, fmt.Errorf("no quoted request found")
	}

	requestLine := matches[0][1] // First quoted part should be the HTTP request
	var bodyStr string

	// Check if there's a second quoted part (request body)
	if len(matches) > 1 {
		bodyStr = matches[1][1] // Second quoted part should be the body
	}

	// Parse the request line: "METHOD /path HTTP/1.1"
	parts := strings.Split(requestLine, " ")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid request line format")
	}

	method := parts[0]
	fullPath := parts[1]

	// Parse URL and query parameters
	parsedURL, err := url.Parse(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	// Create the HTTP request
	var body io.Reader
	if bodyStr != "" {
		body = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequest(method, parsedURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set content type based on body format
	if bodyStr != "" {
		if strings.HasPrefix(strings.TrimSpace(bodyStr), "{") {
			req.Header.Set("Content-Type", "application/json")
		} else {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}

	return req, nil
}

// extractQuery uses the same logic as the proxy server to extract queries
func (qa *QueryAnalyzer) extractQuery(r *http.Request) (string, string, error) {
	if r.Method == "GET" {
		return r.URL.Query().Get("q"), r.URL.Query().Get("db"), nil
	}

	// For POST requests, read the body
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return "", "", fmt.Errorf("failed to read request body: %w", err)
		}
	}

	contentType := r.Header.Get("Content-Type")

	if contentType == "application/x-www-form-urlencoded" {
		values, err := url.ParseQuery(string(bodyBytes))
		if err != nil {
			return "", "", fmt.Errorf("failed to parse form-encoded request body: %w", err)
		}
		return values.Get("q"), values.Get("db"), nil
	}

	if contentType == "application/json" {
		var req struct {
			Query    string `json:"q"`
			Database string `json:"db"`
		}
		if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&req); err != nil {
			return "", "", fmt.Errorf("failed to parse JSON request body: %w", err)
		}
		return req.Query, req.Database, nil
	}

	// Default to form parsing for backward compatibility
	values, err := url.ParseQuery(string(bodyBytes))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse request body as form data: %w", err)
	}
	return values.Get("q"), values.Get("db"), nil
}

func (qa *QueryAnalyzer) extractTimestamp(line string) string {
	// Common timestamp patterns, prioritized by your log format
	patterns := []string{
		`\[\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4}\]`, // Apache/Common Log Format: [02/Aug/2025:17:10:00 +0000]
		`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?`,   // ISO format
		`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`,               // Standard format
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		match := re.FindString(line)
		if match != "" {
			return match
		}
	}
	return ""
}

func (qa *QueryAnalyzer) extractMethod(line string) string {
	// Look for HTTP method after the timestamp and before the path
	re := regexp.MustCompile(`"(GET|POST|PUT|DELETE|HEAD|OPTIONS)\s`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (qa *QueryAnalyzer) extractClientIP(line string) string {
	// Look for IP addresses at the beginning of log lines
	// Handle single IP or comma-separated IPs (for X-Forwarded-For)
	re := regexp.MustCompile(`^(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})(?:,\s*\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})*`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (qa *QueryAnalyzer) extractStatusCode(line string) int {
	// Look for HTTP status code (3-digit number after the HTTP method and path)
	re := regexp.MustCompile(`"\s+(\d{3})\s+`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		if code, err := strconv.Atoi(matches[1]); err == nil {
			return code
		}
	}
	return 0
}

func (qa *QueryAnalyzer) extractResponseTime(line string) string {
	// Look for response time at the end of the log line (typically in microseconds)
	re := regexp.MustCompile(`\s+(\d+)\s*$`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (qa *QueryAnalyzer) analyzeQuery(logEntry LogEntry) *AnalysisResult {
	// Use the existing query filter to validate the query
	filterResult := qa.queryFilter.ValidateQuery(logEntry.Query)

	// If query is blocked by the filter rules, log it as an issue
	if !filterResult.Allowed {
		return &AnalysisResult{
			LogEntry:      logEntry,
			HasTimeFilter: false, // The filter already determined this
			Issue:         "BLOCKED_BY_FILTER",
			Reason:        filterResult.Reason,
		}
	}

	// If the query passed all validations, we don't need to report it
	return nil
}

func writeResults(results []AnalysisResult, outputFilePath string) error {
	file, err := os.Create(outputFilePath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	output := map[string]interface{}{
		"analysis_summary": map[string]interface{}{
			"total_problematic_queries": len(results),
			"analysis_timestamp":        time.Now().Format(time.RFC3339),
		},
		"problematic_queries": results,
	}

	return encoder.Encode(output)
}

func printSummary(results []AnalysisResult) {
	issueCount := make(map[string]int)

	for _, result := range results {
		issueCount[result.Issue]++
	}

	fmt.Println("\nSummary:")
	fmt.Println("========")
	for issue, count := range issueCount {
		fmt.Printf("%-20s: %d queries\n", issue, count)
	}
}
