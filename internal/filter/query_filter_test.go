package filter

import (
	"strings"
	"testing"

	"influxql-analyzer/internal/config"

	"github.com/influxdata/influxql"
)

func TestQueryFilter_ValidateQuery(t *testing.T) {
	rules := config.FilteringRules{
		RequireTimeFilter:     true,
		MaxTimeRangeHours:     24,
		BlockWildcardSelect:   true,
		BlockUnlimitedGroupBy: true,
		BlockExpensiveShows:   true,
		MaxShowSeriesLimit:    100,
		BlockedFunctions:      []string{"count(*)"},
	}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name     string
		query    string
		expected bool
		reason   string
	}{
		{
			name:     "Valid query with time filter",
			query:    "SELECT value FROM mydb..mymeasurement WHERE time > '2023-01-01T00:00:00Z' AND time < '2023-01-02T00:00:00Z'",
			expected: true,
		},
		{
			name:     "Query without time filter",
			query:    "SELECT value FROM mydb..mymeasurement",
			expected: false,
			reason:   "Query must include a time filter",
		},
		{
			name:     "Query with blocked function",
			query:    "SELECT count(*) FROM mydb..mymeasurement WHERE time > '2023-01-01T00:00:00Z' AND time < '2023-01-02T00:00:00Z'",
			expected: false,
			reason:   "Query contains blocked function: count(*)",
		},
		{
			name:     "Valid SHOW DATABASES",
			query:    "SHOW DATABASES",
			expected: true,
		},
		{
			name:     "Query with time filter in subquery should be allowed",
			query:    `SELECT sum("last_total") FROM (SELECT last("total_entities") AS "last_total" FROM "outstanding_orders" WHERE time > now() - 30m AND "status" =~ /created|temporary_unfulfillable|pending|inventory_awaited$/ AND "bin_tags" =~ /^$bin_tags$/ GROUP BY "bin_tags", "status")`,
			expected: true,
		},
		{
			name:     "Query with subquery but no time filter should be blocked",
			query:    `SELECT sum("last_total") FROM (SELECT last("total_entities") AS "last_total" FROM "outstanding_orders" WHERE "status" =~ /created/ GROUP BY "bin_tags", "status")`,
			expected: false,
			reason:   "Query must include a time filter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.ValidateQuery(tt.query)
			if result.Allowed != tt.expected {
				t.Errorf("ValidateQuery() = %v, expected %v", result.Allowed, tt.expected)
			}
			if !tt.expected && tt.reason != "" {
				if result.Reason == "" || result.Reason != tt.reason {
					t.Errorf("Expected reason to contain '%s', got '%s'", tt.reason, result.Reason)
				}
			}
		})
	}
}

func TestQueryFilter_TimeRange(t *testing.T) {
	rules := config.FilteringRules{
		RequireTimeFilter: true,
		MaxTimeRangeHours: 1, // 1 hour max
	}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{
			name:     "Time range within limit",
			query:    "SELECT value FROM mydb..mymeasurement WHERE time > '2023-01-01T00:00:00Z' AND time < '2023-01-01T00:30:00Z'",
			expected: true,
		},
		{
			name:     "Time range within limit 2",
			query:    "SELECT value FROM mydb..mymeasurement WHERE time > now() - 59m limit 1",
			expected: true,
		},
		{
			name:     "Time range within limit 3",
			query:    "SELECT value FROM mydb..mymeasurement WHERE time > now() - 62m and xd=1 and time < now() - 3m limit 1",
			expected: true,
		},
		{
			name:     "Time range exceeds limit",
			query:    "SELECT value FROM mydb..mymeasurement WHERE time > '2023-01-01T00:00:00Z' AND time < '2023-03-02T00:00:00Z'",
			expected: false,
		},
		{
			name:     "Time range exceeds limit 2",
			query:    "SELECT value FROM mydb..mymeasurement WHERE time > now() - 30d limit 1",
			expected: false,
		},
		{
			name:     "Time range exceeds limit 2_2",
			query:    "SELECT value FROM mydb..mymeasurement WHERE time > now() - 30d AND time < now() - 10d LIMIT 1",
			expected: false,
		},
		{
			name:     "Time range exceeds limit 3",
			query:    "SELECT value FROM mydb..mymeasurement WHERE time > 1750614600000000000 LIMIT 1",
			expected: false,
		},
		{
			name:     "Time range exceeds limit 3",
			query:    "SELECT value FROM mydb..mymeasurement WHERE time > 1750614600000000000 LIMIT 1",
			expected: false,
		},
		{
			name:     "Time range exceeds limit 4",
			query:    "SELECT value FROM mymeasurement WHERE time > 1750617000000000000 AND time < 1751826600000000000 LIMIT 1",
			expected: false,
		},
		{
			name:     "Time range with 90d should exceed 1 hour limit",
			query:    "SELECT COUNT(DISTINCT(pps_id)) FROM pps_data WHERE time > now() - 90d AND installation_id =~ /^qa3-adisinglebulkqa300$/ AND mode = 'pick' AND front_logged_in = 'true' AND status = 'open'",
			expected: false,
		},
		{
			name:     "Subquery with 90d time range should exceed 1 hour limit",
			query:    "SELECT sum(\"last_total\") FROM (SELECT last(\"total_entities\") AS \"last_total\" FROM \"outstanding_orders\" WHERE time > now() - 90d GROUP BY \"bin_tags\", \"status\")",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.ValidateQuery(tt.query)
			if result.Allowed != tt.expected {
				t.Errorf("ValidateQuery() = %v, expected %v. Reason: %s", result.Allowed, tt.expected, result.Reason)
			}
		})
	}
}

func TestQueryFilter_AllowedMeasurements(t *testing.T) {
	// Test case 1: Empty allowed_measurements list - normal filtering applies
	rules := config.FilteringRules{
		RequireTimeFilter:   true,
		MaxTimeRangeHours:   24,
		AllowedMeasurements: []string{}, // Empty list
	}
	filter := NewQueryFilter(rules)

	result := filter.ValidateQuery("SELECT value FROM measurement1")
	if result.Allowed {
		t.Errorf("Expected query to be blocked due to missing time filter, but it was allowed")
	}

	// Test case 2: Query with allowed measurement - should bypass filtering
	rules.AllowedMeasurements = []string{"cpu", "memory"}
	filter = NewQueryFilter(rules)

	result = filter.ValidateQuery("SELECT value FROM cpu")
	if !result.Allowed {
		t.Errorf("Expected query on allowed measurement to bypass filters, but it was blocked: %s", result.Reason)
	}
	if !strings.Contains(result.Reason, "allowed measurements") {
		t.Errorf("Expected reason to mention allowed measurements, got: %s", result.Reason)
	}

	// Test case 3: Query with disallowed measurement - normal filtering applies
	result = filter.ValidateQuery("SELECT value FROM disk")
	if result.Allowed {
		t.Errorf("Expected query on disallowed measurement to be blocked due to missing time filter, but it was allowed")
	}

	// Test case 4: Query with multiple allowed measurements - should bypass filtering
	result = filter.ValidateQuery("SELECT value FROM cpu; SELECT value FROM memory")
	if !result.Allowed {
		t.Errorf("Expected query with all allowed measurements to bypass filters, but it was blocked: %s", result.Reason)
	}

	// Test case 5: Query with mixed allowed/disallowed measurements - normal filtering applies
	result = filter.ValidateQuery("SELECT value FROM cpu; SELECT value FROM disk")
	if result.Allowed {
		t.Errorf("Expected query with mixed measurements to be blocked due to missing time filter, but it was allowed")
	}

	// Test case 6: SHOW statements with allowed measurements
	result = filter.ValidateQuery("SHOW SERIES FROM cpu")
	if !result.Allowed {
		t.Errorf("Expected SHOW statement on allowed measurement to bypass filters, but it was blocked: %s", result.Reason)
	}

	// Test case 7: Query without any measurements - normal filtering applies
	result = filter.ValidateQuery("SHOW DATABASES")
	if !result.Allowed {
		t.Errorf("Expected SHOW DATABASES to be allowed (no measurements to check), but it was blocked: %s", result.Reason)
	}
}

func TestQueryFilter_ExtractMeasurements(t *testing.T) {
	rules := config.FilteringRules{}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name        string
		queryString string
		expected    []string
	}{
		{
			name:        "Simple SELECT query",
			queryString: "SELECT value FROM cpu",
			expected:    []string{"cpu"},
		},
		{
			name:        "Query with database.retention.measurement",
			queryString: "SELECT value FROM mydb.autogen.cpu",
			expected:    []string{"cpu"},
		},
		{
			name:        "Query with database..measurement",
			queryString: "SELECT value FROM mydb..cpu",
			expected:    []string{"cpu"},
		},
		{
			name:        "Query with multiple measurements",
			queryString: "SELECT value FROM cpu; SELECT value FROM memory",
			expected:    []string{"cpu", "memory"},
		},
		{
			name:        "SHOW SERIES query",
			queryString: "SHOW SERIES FROM cpu",
			expected:    []string{"cpu"},
		},
		{
			name:        "Query without measurement",
			queryString: "SHOW DATABASES",
			expected:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := influxql.ParseQuery(tt.queryString)
			if err != nil {
				t.Fatalf("Failed to parse query: %v", err)
			}

			measurements := filter.extractMeasurements(query)

			if len(measurements) != len(tt.expected) {
				t.Errorf("Expected %d measurements, got %d: %v", len(tt.expected), len(measurements), measurements)
				return
			}

			// Convert to maps for easier comparison (order doesn't matter)
			expectedMap := make(map[string]bool)
			for _, m := range tt.expected {
				expectedMap[m] = true
			}

			actualMap := make(map[string]bool)
			for _, m := range measurements {
				actualMap[m] = true
			}

			for expected := range expectedMap {
				if !actualMap[expected] {
					t.Errorf("Expected measurement '%s' not found in result: %v", expected, measurements)
				}
			}

			for actual := range actualMap {
				if !expectedMap[actual] {
					t.Errorf("Unexpected measurement '%s' found in result: %v", actual, measurements)
				}
			}
		})
	}
}

func TestQueryFilter_WildcardSelect(t *testing.T) {
	rules := config.FilteringRules{
		BlockWildcardSelect: true,
	}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name     string
		query    string
		expected bool
		reason   string
	}{
		{
			name:     "Wildcard select without LIMIT should be blocked",
			query:    "SELECT * FROM cpu",
			expected: false,
			reason:   "Query contains potentially expensive operations without proper constraints",
		},
		{
			name:     "Wildcard select with LIMIT should be allowed",
			query:    "SELECT * FROM cpu LIMIT 100",
			expected: true,
		},
		{
			name:     "Non-wildcard select should be allowed",
			query:    "SELECT value FROM cpu",
			expected: true,
		},
		{
			name:     "Wildcard select with GROUP BY TIME and LIMIT should be allowed",
			query:    "SELECT * FROM cpu WHERE time > now() - 1h GROUP BY time(5m) LIMIT 100",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.ValidateQuery(tt.query)
			if result.Allowed != tt.expected {
				t.Errorf("ValidateQuery() = %v, expected %v. Reason: %s", result.Allowed, tt.expected, result.Reason)
			}
			if !tt.expected && tt.reason != "" {
				if !strings.Contains(result.Reason, tt.reason) {
					t.Errorf("Expected reason to contain '%s', got '%s'", tt.reason, result.Reason)
				}
			}
		})
	}
}

func TestQueryFilter_UnlimitedGroupBy(t *testing.T) {
	rules := config.FilteringRules{
		BlockUnlimitedGroupBy: true,
	}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name     string
		query    string
		expected bool
		reason   string
	}{
		{
			name:     "GROUP BY without LIMIT or time bucketing should be blocked",
			query:    "SELECT mean(value) FROM cpu GROUP BY host",
			expected: false,
			reason:   "Query contains potentially expensive operations without proper constraints",
		},
		{
			name:     "GROUP BY with LIMIT should be allowed",
			query:    "SELECT mean(value) FROM cpu GROUP BY host LIMIT 100",
			expected: true,
		},
		{
			name:     "GROUP BY with time bucketing should be allowed",
			query:    "SELECT mean(value) FROM cpu WHERE time > now() - 1h GROUP BY time(5m)",
			expected: true,
		},
		{
			name:     "GROUP BY with both time bucketing and LIMIT should be allowed",
			query:    "SELECT mean(value) FROM cpu WHERE time > now() - 1h GROUP BY time(5m), host LIMIT 1000",
			expected: true,
		},
		{
			name:     "Query without GROUP BY should be allowed",
			query:    "SELECT value FROM cpu",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.ValidateQuery(tt.query)
			if result.Allowed != tt.expected {
				t.Errorf("ValidateQuery() = %v, expected %v. Reason: %s", result.Allowed, tt.expected, result.Reason)
			}
			if !tt.expected && tt.reason != "" {
				if !strings.Contains(result.Reason, tt.reason) {
					t.Errorf("Expected reason to contain '%s', got '%s'", tt.reason, result.Reason)
				}
			}
		})
	}
}

func TestQueryFilter_ExpensiveShows(t *testing.T) {
	rules := config.FilteringRules{
		BlockExpensiveShows: true,
		MaxShowSeriesLimit:  1000,
	}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name     string
		query    string
		expected bool
		reason   string
	}{
		{
			name:     "SHOW SERIES without LIMIT should be blocked",
			query:    "SHOW SERIES",
			expected: false,
			reason:   "SHOW SERIES queries must include a LIMIT clause",
		},
		{
			name:     "SHOW SERIES with LIMIT within max should be allowed",
			query:    "SHOW SERIES LIMIT 500",
			expected: true,
		},
		{
			name:     "SHOW SERIES with LIMIT exceeding max should be blocked",
			query:    "SHOW SERIES LIMIT 2000",
			expected: false,
			reason:   "SHOW SERIES LIMIT cannot exceed 1000",
		},
		{
			name:     "SHOW SERIES FROM measurement without LIMIT should be blocked",
			query:    "SHOW SERIES FROM cpu",
			expected: false,
			reason:   "SHOW SERIES queries must include a LIMIT clause",
		},
		{
			name:     "SHOW SERIES FROM measurement with LIMIT should be allowed",
			query:    "SHOW SERIES FROM cpu LIMIT 100",
			expected: true,
		},
		{
			name:     "Other SHOW statements should be allowed",
			query:    "SHOW DATABASES",
			expected: true,
		},
		{
			name:     "SHOW MEASUREMENTS should be allowed",
			query:    "SHOW MEASUREMENTS",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.ValidateQuery(tt.query)
			if result.Allowed != tt.expected {
				t.Errorf("ValidateQuery() = %v, expected %v. Reason: %s", result.Allowed, tt.expected, result.Reason)
			}
			if !tt.expected && tt.reason != "" {
				if !strings.Contains(result.Reason, tt.reason) {
					t.Errorf("Expected reason to contain '%s', got '%s'", tt.reason, result.Reason)
				}
			}
		})
	}
}

func TestQueryFilter_BlockedStatements(t *testing.T) {
	rules := config.FilteringRules{
		BlockedStatements: []string{"DELETE", "DROP"},
	}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name     string
		query    string
		expected bool
		reason   string
	}{
		{
			name:     "DELETE statement should be blocked",
			query:    "DELETE FROM cpu WHERE time < now() - 1d",
			expected: false,
			reason:   "Statement type 'DELETE' not allowed",
		},
		{
			name:     "DROP MEASUREMENT should be blocked",
			query:    "DROP MEASUREMENT cpu",
			expected: false,
			reason:   "Statement type 'DROP' not allowed",
		},
		{
			name:     "DROP DATABASE should be blocked",
			query:    "DROP DATABASE mydb",
			expected: false,
			reason:   "Statement type 'DROP' not allowed",
		},
		{
			name:     "SELECT statement should be allowed",
			query:    "SELECT value FROM cpu",
			expected: true,
		},
		{
			name:     "SHOW statement should be allowed",
			query:    "SHOW DATABASES",
			expected: true,
		},
		{
			name:     "CREATE statement should be allowed if not blocked",
			query:    "CREATE DATABASE testdb",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.ValidateQuery(tt.query)
			if result.Allowed != tt.expected {
				t.Errorf("ValidateQuery() = %v, expected %v. Reason: %s", result.Allowed, tt.expected, result.Reason)
			}
			if !tt.expected && tt.reason != "" {
				if !strings.Contains(result.Reason, tt.reason) {
					t.Errorf("Expected reason to contain '%s', got '%s'", tt.reason, result.Reason)
				}
			}
		})
	}
}

func TestQueryFilter_BlockedFunctions(t *testing.T) {
	rules := config.FilteringRules{
		BlockedFunctions: []string{"count(*)", "distinct", "sample"},
	}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name     string
		query    string
		expected bool
		reason   string
	}{
		{
			name:     "Query with count(*) should be blocked",
			query:    "SELECT count(*) FROM cpu",
			expected: false,
			reason:   "Query contains blocked function: count(*)",
		},
		{
			name:     "Query with DISTINCT should be blocked",
			query:    "SELECT distinct(host) FROM cpu",
			expected: false,
			reason:   "Query contains blocked function: distinct",
		},
		{
			name:     "Query with SAMPLE should be blocked",
			query:    "SELECT sample(value, 10) FROM cpu",
			expected: false,
			reason:   "Query contains blocked function: sample",
		},
		{
			name:     "Query with allowed functions should pass",
			query:    "SELECT mean(value), max(value) FROM cpu",
			expected: true,
		},
		{
			name:     "Query with count but not count(*) should pass",
			query:    "SELECT count(value) FROM cpu",
			expected: true,
		},
		{
			name:     "Case insensitive function blocking",
			query:    "SELECT COUNT(*) FROM cpu",
			expected: false,
			reason:   "Query contains blocked function: count(*)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.ValidateQuery(tt.query)
			if result.Allowed != tt.expected {
				t.Errorf("ValidateQuery() = %v, expected %v. Reason: %s", result.Allowed, tt.expected, result.Reason)
			}
			if !tt.expected && tt.reason != "" {
				if !strings.Contains(result.Reason, tt.reason) {
					t.Errorf("Expected reason to contain '%s', got '%s'", tt.reason, result.Reason)
				}
			}
		})
	}
}

func TestQueryFilter_TimeRangeValidation(t *testing.T) {
	rules := config.FilteringRules{
		RequireTimeFilter: true,
		MaxTimeRangeHours: 2, // 2 hours max
	}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name     string
		query    string
		expected bool
		reason   string
	}{
		{
			name:     "Query without time filter should be blocked",
			query:    "SELECT value FROM cpu",
			expected: false,
			reason:   "Query must include a time filter",
		},
		{
			name:     "Query with valid time range should be allowed",
			query:    "SELECT value FROM cpu WHERE time > now() - 1h",
			expected: true,
		},
		{
			name:     "Query with time range exceeding limit should be blocked",
			query:    "SELECT value FROM cpu WHERE time > now() - 5h",
			expected: false,
			reason:   "Time range",
		},
		{
			name:     "Query with absolute time within range should be allowed",
			query:    "SELECT value FROM cpu WHERE time > '2023-01-01T10:00:00Z' AND time < '2023-01-01T11:00:00Z'",
			expected: true,
		},
		{
			name:     "Query with absolute time exceeding range should be blocked",
			query:    "SELECT value FROM cpu WHERE time > '2023-01-01T10:00:00Z' AND time < '2023-01-02T10:00:00Z'",
			expected: false,
			reason:   "Time range",
		},
		{
			name:     "SHOW DATABASES should bypass time filter requirement",
			query:    "SHOW DATABASES",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.ValidateQuery(tt.query)
			if result.Allowed != tt.expected {
				t.Errorf("ValidateQuery() = %v, expected %v. Reason: %s", result.Allowed, tt.expected, result.Reason)
			}
			if !tt.expected && tt.reason != "" {
				if !strings.Contains(result.Reason, tt.reason) {
					t.Errorf("Expected reason to contain '%s', got '%s'", tt.reason, result.Reason)
				}
			}
		})
	}
}

func TestQueryFilter_CombinedRules(t *testing.T) {
	rules := config.FilteringRules{
		RequireTimeFilter:     true,
		MaxTimeRangeHours:     24,
		BlockWildcardSelect:   true,
		BlockUnlimitedGroupBy: true,
		BlockExpensiveShows:   true,
		MaxShowSeriesLimit:    1000,
		AllowedMeasurements:   []string{"cpu", "memory"},
		BlockedFunctions:      []string{"count(*)"},
		BlockedStatements:     []string{"DELETE", "DROP"},
	}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name     string
		query    string
		expected bool
		reason   string
	}{
		{
			name:     "Query on allowed measurement should bypass all filters",
			query:    "SELECT * FROM cpu",
			expected: true,
			reason:   "allowed measurements",
		},
		{
			name:     "Query on disallowed measurement should apply all filters",
			query:    "SELECT * FROM disk",
			expected: false,
			reason:   "Query must include a time filter",
		},
		{
			name:     "Complex query on allowed measurement should bypass filters",
			query:    "SELECT count(*) FROM cpu GROUP BY host",
			expected: true,
			reason:   "allowed measurements",
		},
		{
			name:     "DELETE on allowed measurement should bypass all filters including statement blocking",
			query:    "DELETE FROM cpu WHERE time < now() - 1d",
			expected: true,
			reason:   "allowed measurements",
		},
		{
			name:     "Valid query on disallowed measurement with proper filters should pass normal validation",
			query:    "SELECT mean(value) FROM disk WHERE time > now() - 1h GROUP BY time(5m) LIMIT 100",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.ValidateQuery(tt.query)
			if result.Allowed != tt.expected {
				t.Errorf("ValidateQuery() = %v, expected %v. Reason: %s", result.Allowed, tt.expected, result.Reason)
			}
			if !tt.expected && tt.reason != "" {
				if !strings.Contains(result.Reason, tt.reason) {
					t.Errorf("Expected reason to contain '%s', got '%s'", tt.reason, result.Reason)
				}
			}
			if tt.expected && tt.reason != "" {
				if !strings.Contains(result.Reason, tt.reason) {
					t.Errorf("Expected reason to contain '%s', got '%s'", tt.reason, result.Reason)
				}
			}
		})
	}
}

func TestQueryFilter_EdgeCases(t *testing.T) {
	rules := config.FilteringRules{
		RequireTimeFilter:   true,
		AllowedMeasurements: []string{"cpu", "memory"},
	}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name     string
		query    string
		expected bool
		reason   string
	}{
		{
			name:     "Empty query should be forwarded to InfluxDB",
			query:    "",
			expected: true,
		},
		{
			name:     "Invalid query should be blocked",
			query:    "INVALID QUERY SYNTAX",
			expected: false,
			reason:   "Invalid query syntax",
		},
		{
			name:     "Query with subquery on allowed measurement",
			query:    "SELECT mean(value) FROM (SELECT value FROM cpu)",
			expected: true,
			reason:   "allowed measurements",
		},
		{
			name:     "Complex query with multiple statements, all on allowed measurements",
			query:    "SELECT value FROM cpu; SELECT value FROM memory",
			expected: true,
			reason:   "allowed measurements",
		},
		{
			name:     "Complex query with mixed allowed/disallowed measurements",
			query:    "SELECT value FROM cpu; SELECT value FROM disk",
			expected: false,
			reason:   "Query must include a time filter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.ValidateQuery(tt.query)
			if result.Allowed != tt.expected {
				t.Errorf("ValidateQuery() = %v, expected %v. Reason: %s", result.Allowed, tt.expected, result.Reason)
			}
			if tt.reason != "" {
				if !strings.Contains(result.Reason, tt.reason) {
					t.Errorf("Expected reason to contain '%s', got '%s'", tt.reason, result.Reason)
				}
			}
		})
	}
}

func TestQueryFilter_OffsetLimit(t *testing.T) {
	rules := config.FilteringRules{
		RequireTimeFilter: false, // Disable time filter requirement for this test
		MaxOffsetLimit:    1000,  // Set max offset limit to 1000
	}
	filter := NewQueryFilter(rules)

	tests := []struct {
		name     string
		query    string
		expected bool
		reason   string
	}{
		{
			name:     "Query with small offset",
			query:    "SELECT value FROM mymeasurement LIMIT 10 OFFSET 100",
			expected: true,
		},
		{
			name:     "Query with offset at limit",
			query:    "SELECT value FROM mymeasurement LIMIT 10 OFFSET 1000",
			expected: true,
		},
		{
			name:     "Query with offset exceeding limit",
			query:    "SELECT value FROM mymeasurement LIMIT 10 OFFSET 1001",
			expected: false,
			reason:   "OFFSET value (1001) exceeds maximum allowed (1000)",
		},
		{
			name:     "Query with very large offset",
			query:    "SELECT value FROM mymeasurement LIMIT 10 OFFSET 999999",
			expected: false,
			reason:   "OFFSET value (999999) exceeds maximum allowed (1000)",
		},
		{
			name:     "Query without offset",
			query:    "SELECT value FROM mymeasurement LIMIT 10",
			expected: true,
		},
		{
			name:     "Query with offset disabled (MaxOffsetLimit = 0)",
			query:    "SELECT value FROM mymeasurement LIMIT 10 OFFSET 999999",
			expected: true, // Will be tested with different rules
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For the last test case, use different rules with MaxOffsetLimit = 0
			testFilter := filter
			if tt.name == "Query with offset disabled (MaxOffsetLimit = 0)" {
				disabledRules := config.FilteringRules{
					RequireTimeFilter: false,
					MaxOffsetLimit:    0, // Disabled
				}
				testFilter = NewQueryFilter(disabledRules)
			}

			result := testFilter.ValidateQuery(tt.query)
			if result.Allowed != tt.expected {
				t.Errorf("ValidateQuery() = %v, expected %v. Reason: %s", result.Allowed, tt.expected, result.Reason)
			}
			if !tt.expected && tt.reason != "" {
				if !strings.Contains(result.Reason, tt.reason) {
					t.Errorf("Expected reason to contain '%s', got '%s'", tt.reason, result.Reason)
				}
			}
		})
	}
}
