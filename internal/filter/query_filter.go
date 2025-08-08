package filter

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/influxdata/influxql"
	log "github.com/sirupsen/logrus"

	"influxql-analyzer/internal/config"
)

// QueryFilter validates and filters InfluxDB queries
type QueryFilter struct {
	rules           config.FilteringRules
	blockedPatterns []*regexp.Regexp
}

// FilterResult represents the result of query filtering
type FilterResult struct {
	Allowed bool
	Reason  string
	Query   string
}

// NewQueryFilter creates a new query filter with the given configuration
func NewQueryFilter(rules config.FilteringRules) *QueryFilter {
	var patterns []*regexp.Regexp

	// Compile regex patterns for blocked functions
	for _, fn := range rules.BlockedFunctions {
		// Use word boundaries for simple function names, but handle special cases like count(*)
		escapedFn := regexp.QuoteMeta(fn)
		var pattern *regexp.Regexp
		if strings.Contains(fn, "(") {
			// For functions with parentheses, don't use word boundaries around the whole thing
			pattern = regexp.MustCompile(`(?i)` + escapedFn)
		} else {
			// For simple identifiers, use word boundaries
			pattern = regexp.MustCompile(`(?i)\b` + escapedFn + `\b`)
		}
		patterns = append(patterns, pattern)
	}

	return &QueryFilter{
		rules:           rules,
		blockedPatterns: patterns,
	}
}

// ValidateQuery validates a query string and returns whether it should be allowed
func (qf *QueryFilter) ValidateQuery(queryString string) FilterResult {
	log.WithField("query", queryString).Debug("Validating query")

	// Parse the query
	query, err := influxql.ParseQuery(queryString)
	if err != nil {
		return FilterResult{
			Allowed: false,
			Reason:  fmt.Sprintf("Invalid query syntax: %v", err),
			Query:   queryString,
		}
	}

	// Check if any measurements in the query are in the allowed list
	// If allowed_measurements is configured and the query contains only allowed measurements,
	// bypass all other filtering
	if len(qf.rules.AllowedMeasurements) > 0 {
		measurements := qf.extractMeasurements(query)
		// Only bypass filtering if:
		// 1. The query contains measurements AND
		// 2. All measurements are in the allowed list
		if len(measurements) > 0 && qf.allMeasurementsAllowed(measurements) {
			return FilterResult{
				Allowed: true,
				Reason:  "Query contains only allowed measurements - bypassing filters",
				Query:   queryString,
			}
		}
		// If allowed_measurements is configured but query doesn't match allowed measurements,
		// or query has no measurements, apply normal filtering
	}

	// Check each statement in the query
	for _, stmt := range query.Statements {
		if result := qf.validateStatement(stmt, queryString); !result.Allowed {
			return result
		}
	}

	// Check for blocked functions using regex
	for i, pattern := range qf.blockedPatterns {
		if pattern.MatchString(queryString) {
			return FilterResult{
				Allowed: false,
				Reason:  fmt.Sprintf("Query contains blocked function: %s", qf.rules.BlockedFunctions[i]),
				Query:   queryString,
			}
		}
	}

	hasRegex := qf.HasRegex(query)

	// Log warning if query contains regex and warning is enabled
	if qf.rules.WarnOnRegexUsage && hasRegex {
		log.WithField("query", queryString).Warn("Query contains regex operators (=~ or !~)")
	}

	if qf.rules.BlockRegexUsage && hasRegex {
		return FilterResult{
			Allowed: false,
			Reason:  "Query contains regex operators (=~ or !~) which are not allowed",
			Query:   queryString,
		}
	}

	return FilterResult{
		Allowed: true,
		Reason:  "Query passed all filters",
		Query:   queryString,
	}
}

// extractMeasurements extracts all measurement names from the parsed query
func (qf *QueryFilter) extractMeasurements(query *influxql.Query) []string {
	var measurements []string
	measurementSet := make(map[string]bool) // Use map to avoid duplicates

	for _, stmt := range query.Statements {
		switch s := stmt.(type) {
		case *influxql.SelectStatement:
			// Extract measurements from Sources (including subqueries)
			qf.extractMeasurementsFromSources(s.Sources, measurementSet)
		case *influxql.ShowSeriesStatement:
			// Extract measurement from SHOW SERIES FROM statement
			if s.Sources != nil {
				for _, source := range s.Sources {
					if measurement, ok := source.(*influxql.Measurement); ok {
						if measurement.Name != "" {
							measurementSet[measurement.Name] = true
						}
					}
				}
			}
		case *influxql.DeleteStatement:
			// Extract measurement from DELETE statement
			if s.Source != nil {
				if measurement, ok := s.Source.(*influxql.Measurement); ok {
					if measurement.Name != "" {
						measurementSet[measurement.Name] = true
					}
				}
			}
		case *influxql.DeleteSeriesStatement:
			// Extract measurements from DELETE FROM statement
			for _, source := range s.Sources {
				if measurement, ok := source.(*influxql.Measurement); ok {
					if measurement.Name != "" {
						measurementSet[measurement.Name] = true
					}
				}
			}
		case *influxql.DropMeasurementStatement:
			// Extract measurement from DROP MEASUREMENT statement
			if s.Name != "" {
				measurementSet[s.Name] = true
			}
		}
	}

	// Convert map to slice
	for measurement := range measurementSet {
		measurements = append(measurements, measurement)
	}

	return measurements
}

// allMeasurementsAllowed checks if all measurements in the list are allowed
func (qf *QueryFilter) allMeasurementsAllowed(measurements []string) bool {
	if len(qf.rules.AllowedMeasurements) == 0 {
		return false
	}

	// Create a map for faster lookup
	allowedSet := make(map[string]bool)
	for _, allowed := range qf.rules.AllowedMeasurements {
		allowedSet[allowed] = true
	}

	// Check if all measurements are in the allowed set
	for _, measurement := range measurements {
		if !allowedSet[measurement] {
			return false
		}
	}

	return true
}

// HasRegex checks if the query contains regex operators (=~ or !~) in SELECT statements
func (qf *QueryFilter) HasRegex(query *influxql.Query) bool {
	for _, stmt := range query.Statements {
		if selectStmt, ok := stmt.(*influxql.SelectStatement); ok {
			if qf.hasRegexInStatement(selectStmt) {
				return true
			}
		}
	}
	return false
}

// hasRegexInStatement checks if a SELECT statement contains regex operators, including in subqueries
func (qf *QueryFilter) hasRegexInStatement(stmt *influxql.SelectStatement) bool {
	// Check the main WHERE clause
	if qf.hasRegexInCondition(stmt.Condition) {
		return true
	}

	// Check subqueries in the FROM clause
	return qf.hasRegexInSources(stmt.Sources)
}

// hasRegexInSources recursively checks for regex operators in sources, including subqueries
func (qf *QueryFilter) hasRegexInSources(sources influxql.Sources) bool {
	for _, source := range sources {
		switch s := source.(type) {
		case *influxql.SubQuery:
			if qf.hasRegexInStatement(s.Statement) {
				return true
			}
		}
	}
	return false
}

// hasRegexInCondition recursively checks for regex operators in expressions
func (qf *QueryFilter) hasRegexInCondition(expr influxql.Expr) bool {
	if expr == nil {
		return false
	}

	switch e := expr.(type) {
	case *influxql.BinaryExpr:
		// Check if this is a regex operator
		if e.Op == influxql.EQREGEX || e.Op == influxql.NEQREGEX {
			return true
		}
		// Recursively check both sides
		return qf.hasRegexInCondition(e.LHS) || qf.hasRegexInCondition(e.RHS)
	case *influxql.ParenExpr:
		return qf.hasRegexInCondition(e.Expr)
	case *influxql.Call:
		// Check function arguments for regex operators
		return slices.ContainsFunc(e.Args, qf.hasRegexInCondition)
	}
	return false
}

func (qf *QueryFilter) validateStatement(stmt influxql.Statement, queryString string) FilterResult {
	// Check if statement type is allowed
	stmtType := qf.getStatementType(stmt)
	if !qf.isStatementAllowed(stmtType) {
		return FilterResult{
			Allowed: false,
			Reason:  fmt.Sprintf("Statement type '%s' not allowed", stmtType),
			Query:   queryString,
		}
	}

	switch s := stmt.(type) {
	case *influxql.SelectStatement:
		return qf.validateSelectStatement(s, queryString)
	case *influxql.ShowSeriesStatement:
		return qf.validateShowSeriesStatement(s, queryString)
	default:
		// Don't block other statement types by default
		return FilterResult{
			Allowed: true,
			Query:   queryString,
		}
	}
}

func (qf *QueryFilter) validateSelectStatement(stmt *influxql.SelectStatement, queryString string) FilterResult {
	// Check if time filter is required and present
	if qf.rules.RequireTimeFilter {
		if !qf.HasTimeFilterInStatement(stmt) {
			return FilterResult{
				Allowed: false,
				Reason:  "Query must include a time filter",
				Query:   queryString,
			}
		}
	}

	// Check time range if time filter exists and max time range is configured
	if qf.rules.RequireTimeFilter && qf.rules.MaxTimeRangeHours > 0 {
		if timeRange := qf.ExtractTimeRangeFromStatement(stmt); timeRange != nil {
			maxDuration := time.Duration(qf.rules.MaxTimeRangeHours) * time.Hour
			warnDuration := time.Duration(qf.rules.WarnQueryDurationHours) * time.Hour
			actualDuration := timeRange.Duration()
			log.WithFields(log.Fields{
				"query":            queryString,
				"actual_duration":  actualDuration,
				"allowed_duration": maxDuration,
			}).Debug("Query time range details")
			if warnDuration > 0 && actualDuration > warnDuration {
				log.WithFields(log.Fields{
					"query":            queryString,
					"actual_duration":  actualDuration,
					"allowed_duration": maxDuration,
					"warn_duration":    warnDuration,
				}).Warn("Query exceeds warning threshold")
			}
			if actualDuration > maxDuration {
				return FilterResult{
					Allowed: false,
					Reason:  fmt.Sprintf("Time range (%v) exceeds maximum allowed (%v)", actualDuration, maxDuration),
					Query:   queryString,
				}
			}
		}
	}

	// Check for large offset values
	if qf.rules.MaxOffsetLimit > 0 && stmt.Offset > qf.rules.MaxOffsetLimit {
		return FilterResult{
			Allowed: false,
			Reason:  fmt.Sprintf("OFFSET value (%d) exceeds maximum allowed (%d)", stmt.Offset, qf.rules.MaxOffsetLimit),
			Query:   queryString,
		}
	}

	// Check for potentially expensive operations
	if qf.isExpensiveQuery(stmt) {
		return FilterResult{
			Allowed: false,
			Reason:  "Query contains potentially expensive operations without proper constraints",
			Query:   queryString,
		}
	}

	return FilterResult{Allowed: true, Query: queryString}
}

func (qf *QueryFilter) validateShowSeriesStatement(stmt *influxql.ShowSeriesStatement, queryString string) FilterResult {
	// Check if expensive SHOW queries are blocked
	if qf.rules.BlockExpensiveShows {
		// SHOW SERIES can be expensive without LIMIT
		if stmt.Limit == 0 {
			return FilterResult{
				Allowed: false,
				Reason:  "SHOW SERIES queries must include a LIMIT clause",
				Query:   queryString,
			}
		}

		// Check if limit is reasonable
		if stmt.Limit > qf.rules.MaxShowSeriesLimit {
			return FilterResult{
				Allowed: false,
				Reason:  fmt.Sprintf("SHOW SERIES LIMIT cannot exceed %d", qf.rules.MaxShowSeriesLimit),
				Query:   queryString,
			}
		}
	}

	return FilterResult{Allowed: true, Query: queryString}
}

func (qf *QueryFilter) hasTimeFilter(condition influxql.Expr) bool {
	if condition == nil {
		return false
	}

	return qf.containsTimeCondition(condition)
}

// HasTimeFilterInStatement checks if a SELECT statement has a time filter, including in subqueries
func (qf *QueryFilter) HasTimeFilterInStatement(stmt *influxql.SelectStatement) bool {
	// Check the main WHERE clause
	if qf.hasTimeFilter(stmt.Condition) {
		return true
	}

	// Check subqueries in the FROM clause
	return qf.hasTimeFilterInSources(stmt.Sources)
}

// hasTimeFilterInSources recursively checks for time filters in sources, including subqueries
func (qf *QueryFilter) hasTimeFilterInSources(sources influxql.Sources) bool {
	for _, source := range sources {
		switch s := source.(type) {
		case *influxql.SubQuery:
			if s.Statement != nil {
				// Recursively check the subquery
				if qf.HasTimeFilterInStatement(s.Statement) {
					return true
				}
			}
		}
	}
	return false
}

func (qf *QueryFilter) containsTimeCondition(expr influxql.Expr) bool {
	switch e := expr.(type) {
	case *influxql.BinaryExpr:
		// Check if this is a time condition
		if ref, ok := e.LHS.(*influxql.VarRef); ok && ref.Val == "time" {
			return true
		}
		// Recursively check both sides
		return qf.containsTimeCondition(e.LHS) || qf.containsTimeCondition(e.RHS)
	case *influxql.ParenExpr:
		return qf.containsTimeCondition(e.Expr)
	case *influxql.Call:
		// Check function arguments for time conditions
		return slices.ContainsFunc(e.Args, qf.containsTimeCondition)
	}
	return false
}

// TimeRange represents a time range with start and end times
type TimeRange struct {
	Start time.Time
	End   time.Time
}

func (tr *TimeRange) Duration() time.Duration {
	return tr.End.Sub(tr.Start)
}

func (qf *QueryFilter) extractTimeRange(condition influxql.Expr) *TimeRange {
	if condition == nil {
		return nil
	}

	var start, end time.Time
	qf.extractTimeConditions(condition, &start, &end)

	// Handle cases where we have both start and end times
	if !start.IsZero() && !end.IsZero() {
		return &TimeRange{Start: start, End: end}
	}

	// Handle cases where we only have a start time - assume end time is now
	if !start.IsZero() && end.IsZero() {
		return &TimeRange{Start: start, End: time.Now()}
	}

	// Handle cases where we only have an end time - this is less common but possible
	if start.IsZero() && !end.IsZero() {
		// For queries like "time < some_time", we need to guess a reasonable start
		// Use a very old time as start to represent "from beginning of time"
		return &TimeRange{Start: time.Unix(0, 0), End: end}
	}

	return nil
}

// ExtractTimeRangeFromStatement extracts time range from a SELECT statement, including subqueries
func (qf *QueryFilter) ExtractTimeRangeFromStatement(stmt *influxql.SelectStatement) *TimeRange {
	// Check the main WHERE clause first
	if timeRange := qf.extractTimeRange(stmt.Condition); timeRange != nil {
		return timeRange
	}

	// Check subqueries in the FROM clause
	return qf.extractTimeRangeFromSources(stmt.Sources)
}

// extractTimeRangeFromSources recursively extracts time ranges from sources, including subqueries
func (qf *QueryFilter) extractTimeRangeFromSources(sources influxql.Sources) *TimeRange {
	for _, source := range sources {
		switch s := source.(type) {
		case *influxql.SubQuery:
			if s.Statement != nil {
				// Recursively check the subquery
				if timeRange := qf.ExtractTimeRangeFromStatement(s.Statement); timeRange != nil {
					return timeRange
				}
			}
		}
	}
	return nil
}

func (qf *QueryFilter) extractTimeConditions(expr influxql.Expr, start, end *time.Time) {
	switch e := expr.(type) {
	case *influxql.BinaryExpr:
		// Check for direct time conditions
		if ref, ok := e.LHS.(*influxql.VarRef); ok && ref.Val == "time" {
			// Handle both StringLiteral and TimeLiteral
			var timeVal time.Time
			var err error

			if timeLit, ok := e.RHS.(*influxql.TimeLiteral); ok {
				timeVal = timeLit.Val
			} else if strLit, ok := e.RHS.(*influxql.StringLiteral); ok {
				timeVal, err = qf.parseTimeString(strLit.Val)
			} else if intLit, ok := e.RHS.(*influxql.IntegerLiteral); ok {
				// Handle integer timestamps (nanoseconds since Unix epoch)
				timeVal = time.Unix(0, intLit.Val)
			} else if binExpr, ok := e.RHS.(*influxql.BinaryExpr); ok {
				// Handle relative time expressions like "now() - 30d"
				timeVal = qf.evaluateTimeExpression(binExpr)
			}

			if err == nil && !timeVal.IsZero() {
				switch e.Op {
				case influxql.GT, influxql.GTE:
					*start = timeVal
				case influxql.LT, influxql.LTE:
					*end = timeVal
				}
			}
		} else if ref, ok := e.RHS.(*influxql.VarRef); ok && ref.Val == "time" {
			// Handle reverse cases: literal < time or literal > time
			var timeVal time.Time
			var err error

			if timeLit, ok := e.LHS.(*influxql.TimeLiteral); ok {
				timeVal = timeLit.Val
			} else if strLit, ok := e.LHS.(*influxql.StringLiteral); ok {
				timeVal, err = qf.parseTimeString(strLit.Val)
			} else if intLit, ok := e.LHS.(*influxql.IntegerLiteral); ok {
				// Handle integer timestamps (nanoseconds since Unix epoch)
				timeVal = time.Unix(0, intLit.Val)
			}

			if err == nil && !timeVal.IsZero() {
				switch e.Op {
				case influxql.LT, influxql.LTE:
					*start = timeVal
				case influxql.GT, influxql.GTE:
					*end = timeVal
				}
			}
		}

		// Always recursively check both sides for nested conditions
		qf.extractTimeConditions(e.LHS, start, end)
		qf.extractTimeConditions(e.RHS, start, end)
	case *influxql.ParenExpr:
		qf.extractTimeConditions(e.Expr, start, end)
	case *influxql.Call:
		// Check function arguments for time conditions
		for _, arg := range e.Args {
			qf.extractTimeConditions(arg, start, end)
		}
	}
}

// evaluateTimeExpression evaluates relative time expressions like "now() - 30d"
func (qf *QueryFilter) evaluateTimeExpression(expr *influxql.BinaryExpr) time.Time {
	// Handle binary expressions like "now() - 30d"
	switch expr.Op {
	case influxql.SUB:
		// Check if LHS is a now() function call
		if call, ok := expr.LHS.(*influxql.Call); ok && call.Name == "now" {
			now := time.Now()
			// Check if RHS is a duration literal
			if duration := qf.parseDuration(expr.RHS); duration > 0 {
				return now.Add(-duration)
			}
		}
	case influxql.ADD:
		// Handle addition: now() + 1h
		if call, ok := expr.LHS.(*influxql.Call); ok && call.Name == "now" {
			now := time.Now()
			if duration := qf.parseDuration(expr.RHS); duration > 0 {
				return now.Add(duration)
			}
		}
	}
	return time.Time{}
}

// parseDuration parses duration strings like "30d", "1h", "5m"
func (qf *QueryFilter) parseDuration(expr influxql.Expr) time.Duration {
	switch e := expr.(type) {
	case *influxql.DurationLiteral:
		return e.Val
	case *influxql.StringLiteral:
		// Use InfluxQL's official ParseDuration function which supports all InfluxDB duration formats
		if d, err := influxql.ParseDuration(e.Val); err == nil {
			return d
		}
		// Fallback to Go's standard parser for compatibility
		if d, err := time.ParseDuration(e.Val); err == nil {
			return d
		}
		return 0
	case *influxql.IntegerLiteral:
		// Assume it's nanoseconds if just a number
		return time.Duration(e.Val)
	}
	return 0
}

func (qf *QueryFilter) isExpensiveQuery(stmt *influxql.SelectStatement) bool {
	// Check for SELECT * without LIMIT (if configured)
	if qf.rules.BlockWildcardSelect && qf.hasWildcardSelect(stmt.Fields) && stmt.Limit == 0 {
		return true
	}

	// Check for GROUP BY without time bucketing and without LIMIT (if configured)
	if qf.rules.BlockUnlimitedGroupBy && len(stmt.Dimensions) > 0 && stmt.Limit == 0 {
		hasTimeBucket := false
		for _, dim := range stmt.Dimensions {
			if call, ok := dim.Expr.(*influxql.Call); ok && call.Name == "time" {
				hasTimeBucket = true
				break
			}
		}
		if !hasTimeBucket {
			return true
		}
	}

	return false
}

func (qf *QueryFilter) hasWildcardSelect(fields influxql.Fields) bool {
	for _, field := range fields {
		if ref, ok := field.Expr.(*influxql.Wildcard); ok && ref != nil {
			return true
		}
	}
	return false
}

// getStatementType returns a string representation of the statement type
func (qf *QueryFilter) getStatementType(stmt influxql.Statement) string {
	switch stmt.(type) {
	case *influxql.SelectStatement:
		return "SELECT"
	case *influxql.ShowSeriesStatement:
		return "SHOW SERIES"
	case *influxql.ShowMeasurementsStatement:
		return "SHOW MEASUREMENTS"
	case *influxql.ShowDatabasesStatement:
		return "SHOW DATABASES"
	case *influxql.ShowTagKeysStatement:
		return "SHOW TAG KEYS"
	case *influxql.ShowTagValuesStatement:
		return "SHOW TAG VALUES"
	case *influxql.ShowFieldKeysStatement:
		return "SHOW FIELD KEYS"
	case *influxql.CreateDatabaseStatement:
		return "CREATE"
	case *influxql.DropDatabaseStatement:
		return "DROP"
	case *influxql.DropMeasurementStatement:
		return "DROP"
	case *influxql.DropRetentionPolicyStatement:
		return "DROP"
	case *influxql.DropSubscriptionStatement:
		return "DROP"
	case *influxql.DropUserStatement:
		return "DROP"
	case *influxql.DeleteSeriesStatement:
		return "DELETE"
	case *influxql.DeleteStatement:
		return "DELETE"
	default:
		return fmt.Sprintf("%T", stmt)
	}
}

// isStatementAllowed checks if a statement type is allowed based on configuration
func (qf *QueryFilter) isStatementAllowed(stmtType string) bool {
	// Check if explicitly blocked
	for _, blocked := range qf.rules.BlockedStatements {
		if strings.EqualFold(blocked, stmtType) {
			return false
		}
	}

	// Allow by default if not explicitly blocked
	return true
}

// extractMeasurementsFromSources recursively extracts measurements from sources, including subqueries
func (qf *QueryFilter) extractMeasurementsFromSources(sources influxql.Sources, measurementSet map[string]bool) {
	for _, source := range sources {
		switch s := source.(type) {
		case *influxql.Measurement:
			if s.Name != "" {
				measurementSet[s.Name] = true
			}
		case *influxql.SubQuery:
			// Recursively extract measurements from subquery
			if s.Statement != nil {
				qf.extractMeasurementsFromSources(s.Statement.Sources, measurementSet)
			}
		}
	}
}

// parseTimeString tries to parse a time string using InfluxQL's time parsing logic
func (qf *QueryFilter) parseTimeString(timeStr string) (time.Time, error) {
	// First, try to use InfluxQL's StringLiteral parsing which handles many formats
	strLit := &influxql.StringLiteral{Val: timeStr}
	if strLit.IsTimeLiteral() {
		timeLit, err := strLit.ToTimeLiteral(time.UTC) // Use UTC as default location
		if err == nil {
			return timeLit.Val, nil
		}
	}

	// Fallback to manual parsing for additional formats
	timeFormats := []string{
		time.RFC3339,                  // 2006-01-02T15:04:05Z07:00
		time.RFC3339Nano,              // 2006-01-02T15:04:05.999999999Z07:00
		"2006-01-02T15:04:05Z",        // 2006-01-02T15:04:05Z
		"2006-01-02T15:04:05.000Z",    // 2006-01-02T15:04:05.000Z
		"2006-01-02T15:04:05.000000Z", // 2006-01-02T15:04:05.000000Z
		"2006-01-02 15:04:05",         // 2006-01-02 15:04:05
		"2006-01-02 15:04:05.000",     // 2006-01-02 15:04:05.000
		"2006-01-02 15:04:05.000000",  // 2006-01-02 15:04:05.000000
		"2006-01-02T15:04:05",         // 2006-01-02T15:04:05
		"2006-01-02T15:04:05.000",     // 2006-01-02T15:04:05.000
		"2006-01-02T15:04:05.000000",  // 2006-01-02T15:04:05.000000
		"2006-01-02",                  // Date only format
	}

	var lastErr error
	for _, format := range timeFormats {
		if t, err := time.Parse(format, timeStr); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}

	return time.Time{}, lastErr
}
