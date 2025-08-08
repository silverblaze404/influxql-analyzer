package config

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v2"
)

// FilteringRules contains configurable query filtering options
type FilteringRules struct {
	RequireTimeFilter      bool          `yaml:"require_time_filter"`
	MaxTimeRangeHours      int           `yaml:"max_time_range_hours"`
	WarnQueryDurationHours int           `yaml:"warn_query_duration_hours"`
	WarnOnRegexUsage       bool          `yaml:"warn_on_regex_usage"`
	BlockRegexUsage        bool          `yaml:"block_regex_usage"`
	BlockWildcardSelect    bool          `yaml:"block_wildcard_select"`
	BlockUnlimitedGroupBy  bool          `yaml:"block_unlimited_group_by"`
	BlockExpensiveShows    bool          `yaml:"block_expensive_shows"`
	MaxShowSeriesLimit     int           `yaml:"max_show_series_limit"`
	MaxOffsetLimit         int           `yaml:"max_offset_limit"` // Maximum allowed OFFSET value in queries
	BlockedFunctions       []string      `yaml:"blocked_functions"`
	BlockedStatements      []string      `yaml:"blocked_statements"`
	AllowedMeasurements    []string      `yaml:"allowed_measurements"`
	Logging                LoggingConfig `yaml:"logging"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Load reads and parses the configuration file
func Load(filename string) (*FilteringRules, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config FilteringRules
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	setDefaults(&config)

	if err := validate(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

func setDefaults(config *FilteringRules) {
	// Set filtering rule defaults
	if config.MaxTimeRangeHours == 0 {
		config.MaxTimeRangeHours = 840 // 35 days
	}
	if config.MaxShowSeriesLimit == 0 {
		config.MaxShowSeriesLimit = 10000
	}
	// MaxOffsetLimit defaults to 0 (disabled) - don't set a default value

	// Set logging defaults
	if config.Logging.Level == "" {
		config.Logging.Level = "info" // Default log level
	}
	if config.Logging.Format == "" {
		config.Logging.Format = "json" // Default log format
	}
}

func validate(config *FilteringRules) error {
	if config.MaxTimeRangeHours < 0 {
		return fmt.Errorf("filtering_rules.max_time_range_hours must be positive")
	}
	if config.MaxShowSeriesLimit < 0 {
		return fmt.Errorf("filtering_rules.max_show_series_limit must be positive")
	}
	if config.MaxOffsetLimit < 0 {
		return fmt.Errorf("filtering_rules.max_offset_limit must be positive")
	}

	// Validate logging configuration
	validLevels := []string{"trace", "debug", "info", "warn", "warning", "error", "fatal", "panic"}
	validLevel := slices.Contains(validLevels, strings.ToLower(config.Logging.Level))
	if !validLevel {
		return fmt.Errorf("logging.level must be one of: %s", strings.Join(validLevels, ", "))
	}

	validFormats := []string{"text", "json"}
	validFormat := slices.Contains(validFormats, strings.ToLower(config.Logging.Format))
	if !validFormat {
		return fmt.Errorf("logging.format must be one of: %s", strings.Join(validFormats, ", "))
	}

	return nil
}

// Returns true if query filtering is disabled
func (p *FilteringRules) IsQueryFilteringDisabled() bool {
	return !p.RequireTimeFilter
}
