package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/estran-studio/logviewer/pkg/ty"
)

// LogEntry represents a single log record.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Level     string    `json:"level"`
	Fields    ty.MI     `json:"fields"`
	ContextID string    `json:"context_id"`
}

// Field provides case-insensitive field access for templates.
// Usage: {{.Field "level"}} or {{.Field "thread"}}
func (e LogEntry) Field(key string) interface{} {
	// Check struct fields first
	switch key {
	case "level", "Level":
		return e.Level
	case "message", "Message":
		return e.Message
	case "timestamp", "Timestamp":
		return e.Timestamp
	}
	// Try Fields map with exact match
	if val, ok := e.Fields[key]; ok {
		return val
	}
	// Try capitalized version
	if len(key) > 0 && key[0] >= 'a' && key[0] <= 'z' {
		capKey := string(key[0]-32) + key[1:]
		if val, ok := e.Fields[capKey]; ok {
			return val
		}
	}
	return ""
}

// LogSearchResult is the result of a search operation.
// It provides methods to retrieve entries, fields, and pagination info.
type LogSearchResult interface {
	GetSearch() *LogSearch
	GetEntries(context context.Context) ([]LogEntry, chan []LogEntry, error)
	GetFields(context context.Context) (ty.UniSet[string], chan ty.UniSet[string], error)
	GetPaginationInfo() *PaginationInfo
	Err() <-chan error
}

// PaginationInfo contains information about available pages of results.
type PaginationInfo struct {
	HasMore       bool
	NextPageToken string
}

// LogBackend is the interface for a log backend (e.g., Splunk, CloudWatch).
type LogBackend interface {
	Get(ctx context.Context, search *LogSearch) (LogSearchResult, error)
	// GetFieldValues returns distinct values for the specified fields.
	// If fields is empty, returns values for all fields.
	// The result maps field names to their distinct values.
	GetFieldValues(ctx context.Context, search *LogSearch, fields []string) (map[string][]string, error)
}

// ExtractJSONFromEntry extracts JSON fields from the entry's Message and populates
// entry.Fields, entry.Level, entry.Message, and entry.Timestamp based on the search
// configuration. This is used by both the reader and printer to avoid code duplication.
// This function is idempotent - it's safe to call multiple times on the same entry.
func ExtractJSONFromEntry(entry *LogEntry, search *LogSearch) {
	if !search.FieldExtraction.JSON.Set || !search.FieldExtraction.JSON.Value {
		return
	}

	// Skip if JSON already extracted (idempotency check)
	// If message doesn't contain '{', it's either already extracted or not JSON
	if !strings.Contains(entry.Message, "{") {
		return
	}

	var jsonMap map[string]interface{}
	var jsonContent string
	// Find the last occurrence of '{' to extract JSON from mixed content
	if idx := strings.LastIndex(entry.Message, "{"); idx != -1 {
		jsonContent = entry.Message[idx:]
	} else {
		return // No JSON found
	}

	decoder := json.NewDecoder(strings.NewReader(jsonContent))
	if err := decoder.Decode(&jsonMap); err != nil {
		return // Not valid JSON, leave entry unchanged
	}

	// Get configured key names or use defaults
	msgKey := "message"
	if search.FieldExtraction.JSONMessageKey.Set {
		msgKey = search.FieldExtraction.JSONMessageKey.Value
	}
	levelKey := "level"
	if search.FieldExtraction.JSONLevelKey.Set {
		levelKey = search.FieldExtraction.JSONLevelKey.Value
	}
	tsKey := "timestamp"
	if search.FieldExtraction.JSONTimestampKey.Set {
		tsKey = search.FieldExtraction.JSONTimestampKey.Value
	}

	// Extract all fields except the special ones
	if entry.Fields == nil {
		entry.Fields = make(ty.MI)
	}
	for k, v := range jsonMap {
		if k == msgKey || k == levelKey || k == tsKey {
			continue
		}
		entry.Fields[k] = v
	}

	// Extract message
	if v, ok := jsonMap[msgKey]; ok {
		if s, ok := v.(string); ok {
			entry.Message = s
		}
	}

	// Extract level
	if v, ok := jsonMap[levelKey]; ok {
		if s, ok := v.(string); ok {
			entry.Level = s
		}
	}

	// Extract timestamp
	if v, ok := jsonMap[tsKey]; ok {
		if parsed, err := parseTimestamp(v); err == nil && !parsed.IsZero() {
			entry.Timestamp = parsed
		}
	}
}

// GetFieldValuesFromResult is a helper function for backends that don't have native
// aggregation support. It extracts field values from a LogSearchResult by iterating
// through all entries. If fields is empty, returns all fields found.
func GetFieldValuesFromResult(ctx context.Context, result LogSearchResult, fields []string) (map[string][]string, error) {
	entries, _, err := result.GetEntries(ctx)
	if err != nil {
		return nil, err
	}

	// Use a map of sets to track unique values per field
	valueSet := make(map[string]map[string]bool)

	for _, entry := range entries {
		// Extract JSON fields if needed
		ExtractJSONFromEntry(&entry, result.GetSearch())

		// If no specific fields requested, collect all fields
		if len(fields) == 0 {
			for k, v := range entry.Fields {
				if valueSet[k] == nil {
					valueSet[k] = make(map[string]bool)
				}
				valueSet[k][fmt.Sprintf("%v", v)] = true
			}
		} else {
			// Only collect values for requested fields
			for _, field := range fields {
				val := entry.Field(field)
				if val != nil && val != "" {
					if valueSet[field] == nil {
						valueSet[field] = make(map[string]bool)
					}
					valueSet[field][fmt.Sprintf("%v", val)] = true
				}
			}
		}
	}

	// Convert sets to slices
	result2 := make(map[string][]string)
	for k, v := range valueSet {
		values := make([]string, 0, len(v))
		for val := range v {
			values = append(values, val)
		}
		result2[k] = values
	}

	return result2, nil
}

// parseTimestamp attempts to parse various timestamp formats
func parseTimestamp(value interface{}) (time.Time, error) {
	var timeStr string
	switch v := value.(type) {
	case string:
		timeStr = v
	case float64:
		// Unix timestamp
		return time.Unix(int64(v), 0), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported timestamp type: %T", value)
	}

	// Try common timestamp formats
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, timeStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse timestamp: %s", timeStr)
}
