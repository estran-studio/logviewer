package printer_test

import (
	"strings"
	"testing"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/printer"
	"github.com/stretchr/testify/assert"
)

func TestExpandJSON(t *testing.T) {
	t.Run("expands simple JSON object", func(t *testing.T) {
		input := "get data from json: {\"key\": \"value\", \"num\": 42}"
		result := printer.ExpandJSON(input)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "key")
		assert.Contains(t, result, "value")
		assert.Contains(t, result, "42")
	})

	t.Run("expands JSON array", func(t *testing.T) {
		input := "Response: [\"item1\", \"item2\", \"item3\"]"
		result := printer.ExpandJSON(input)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "item1")
		assert.Contains(t, result, "item2")
	})

	t.Run("expands nested JSON", func(t *testing.T) {
		input := "Payload: {\"user\": {\"name\": \"John\", \"id\": 123}, \"active\": true}"
		result := printer.ExpandJSON(input)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "user")
		assert.Contains(t, result, "name")
		assert.Contains(t, result, "John")
	})

	t.Run("expands multiple JSON objects", func(t *testing.T) {
		input := "First: {\"a\": 1} Second: {\"b\": 2}"
		result := printer.ExpandJSON(input)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "a")
		assert.Contains(t, result, "b")
	})

	t.Run("ignores empty JSON objects", func(t *testing.T) {
		input := "Empty object: {}"
		result := printer.ExpandJSON(input)
		assert.Empty(t, result)
	})

	t.Run("ignores empty JSON arrays", func(t *testing.T) {
		input := "Empty array: []"
		result := printer.ExpandJSON(input)
		assert.Empty(t, result)
	})

	t.Run("returns empty for no JSON", func(t *testing.T) {
		input := "This is just a plain log message"
		result := printer.ExpandJSON(input)
		assert.Empty(t, result)
	})

	t.Run("handles JSON with special characters", func(t *testing.T) {
		input := "Message: {\"url\": \"https://example.com?param=value&other=123\"}"
		result := printer.ExpandJSON(input)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "url")
	})

	t.Run("handles JSON with escaped quotes", func(t *testing.T) {
		input := "Data: {\"message\": \"He said \\\"hello\\\" to me\"}"
		result := printer.ExpandJSON(input)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "message")
	})

	t.Run("handles real-world checkout log", func(t *testing.T) {
		input := "Outbound: {\"redirectUrl\":\"https://payments.example.com\",\"sessionId\":\"ABC123\"}"
		result := printer.ExpandJSON(input)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "redirectUrl")
		assert.Contains(t, result, "sessionId")
	})
}

func TestExpandJSONLimit(t *testing.T) {
	t.Run("respects line limit", func(t *testing.T) {
		// Create a JSON with many fields (will be many lines when formatted)
		input := `Data: {"field1": "value1", "field2": "value2", "field3": "value3", "field4": "value4", "field5": "value5"}`
		result := printer.ExpandJSONLimit(input, 3)

		lines := len(strings.Split(result, "\n"))
		// Should have at most 4 lines (3 + truncation message)
		assert.LessOrEqual(t, lines, 5)
		assert.Contains(t, result, "truncated")
	})

	t.Run("no truncation for short JSON", func(t *testing.T) {
		input := "Data: {\"key\": \"value\"}"
		result := printer.ExpandJSONLimit(input, 10)

		assert.NotEmpty(t, result)
		assert.NotContains(t, result, "truncated")
	})

	t.Run("returns empty for no JSON", func(t *testing.T) {
		input := "No JSON here"
		result := printer.ExpandJSONLimit(input, 5)
		assert.Empty(t, result)
	})
}

func TestExpandJSONCompact(t *testing.T) {
	t.Run("formats JSON on single line", func(t *testing.T) {
		input := "Data: {\"key\": \"value\", \"num\": 42}"
		result := printer.ExpandJSONCompact(input)

		assert.NotEmpty(t, result)
		// Should have minimal newlines (only leading newline per JSON)
		lines := strings.Split(strings.TrimSpace(result), "\n")
		assert.LessOrEqual(t, len(lines), 2) // At most 2 lines for single JSON
	})

	t.Run("formats array on single line", func(t *testing.T) {
		input := "Array: [\"a\", \"b\", \"c\"]"
		result := printer.ExpandJSONCompact(input)

		assert.NotEmpty(t, result)
		assert.Contains(t, result, "a")
		lines := strings.Split(strings.TrimSpace(result), "\n")
		assert.LessOrEqual(t, len(lines), 2)
	})

	t.Run("returns empty for no JSON", func(t *testing.T) {
		input := "No JSON here"
		result := printer.ExpandJSONCompact(input)
		assert.Empty(t, result)
	})
}

func TestExpandJSONLimitDepth(t *testing.T) {
	t.Run("limits depth to 1 level", func(t *testing.T) {
		input := `Data: {"level1": {"level2": {"level3": "deep"}}}`
		result := printer.ExpandJSONLimitDepth(input, 1)

		assert.NotEmpty(t, result)
		assert.Contains(t, result, "level1")
		assert.Contains(t, result, "...")
		assert.NotContains(t, result, "level3")
	})

	t.Run("limits depth to 2 levels", func(t *testing.T) {
		input := `Data: {"a": {"b": {"c": {"d": "value"}}}}`
		result := printer.ExpandJSONLimitDepth(input, 2)

		assert.NotEmpty(t, result)
		assert.Contains(t, result, "a")
		assert.Contains(t, result, "b")
		assert.Contains(t, result, "...")
		assert.NotContains(t, result, "d")
	})

	t.Run("handles arrays with depth limit", func(t *testing.T) {
		input := `Data: {"items": [{"nested": {"deep": "value"}}]}`
		result := printer.ExpandJSONLimitDepth(input, 3)

		assert.NotEmpty(t, result)
		assert.Contains(t, result, "items")
		assert.Contains(t, result, "nested")
		assert.Contains(t, result, "...")
		// With depth=3: root(0) -> items(1) -> array_object(2) -> nested(3) -> deep(4, truncated)
	})

	t.Run("preserves shallow JSON", func(t *testing.T) {
		input := `Data: {"key1": "value1", "key2": "value2"}`
		result := printer.ExpandJSONLimitDepth(input, 3)

		assert.NotEmpty(t, result)
		assert.Contains(t, result, "key1")
		assert.Contains(t, result, "value1")
		assert.Contains(t, result, "key2")
		assert.NotContains(t, result, "...")
	})

	t.Run("depth 0 replaces everything with ...", func(t *testing.T) {
		input := `Data: {"key": "value"}`
		result := printer.ExpandJSONLimitDepth(input, 0)

		assert.NotEmpty(t, result)
		assert.Contains(t, result, "...")
		assert.NotContains(t, result, "key")
	})

	t.Run("handles complex nested structure", func(t *testing.T) {
		input := `Response: {"user": {"name": "John", "profile": {"age": 30, "address": {"city": "NYC"}}}}`
		result := printer.ExpandJSONLimitDepth(input, 2)

		assert.NotEmpty(t, result)
		assert.Contains(t, result, "user")
		assert.Contains(t, result, "name")
		assert.Contains(t, result, "profile")
		assert.Contains(t, result, "...")
		assert.NotContains(t, result, "city")
	})

	t.Run("returns empty for no JSON", func(t *testing.T) {
		input := "No JSON here"
		result := printer.ExpandJSONLimitDepth(input, 2)
		assert.Empty(t, result)
	})

	t.Run("handles real-world nested payload", func(t *testing.T) {
		input := `Outbound: {"redirectUrl":"https://example.com","paymentSessionId":"ABC","details":{"amount":100,"currency":"USD","items":[{"id":1,"name":"ticket"}]}}`
		result := printer.ExpandJSONLimitDepth(input, 2)

		assert.NotEmpty(t, result)
		assert.Contains(t, result, "redirectUrl")
		assert.Contains(t, result, "details")
		assert.Contains(t, result, "...")
		// Items array at depth 3 should be truncated
	})
}

func TestFormatTimestamp(t *testing.T) {
	t.Run("formats valid timestamp in local time", func(t *testing.T) {
		// Use local time to ensure test works regardless of timezone
		ts := time.Date(2025, 12, 17, 10, 30, 45, 0, time.Local)
		result := printer.FormatTimestamp(ts, "15:04:05")
		assert.Equal(t, "10:30:45", result)
	})

	t.Run("converts UTC to local time", func(t *testing.T) {
		ts := time.Date(2025, 12, 17, 10, 30, 45, 0, time.UTC)
		result := printer.FormatTimestamp(ts, "15:04:05")
		// Result should be the local time equivalent
		expected := ts.Local().Format("15:04:05")
		assert.Equal(t, expected, result)
	})

	t.Run("returns N/A for zero timestamp", func(t *testing.T) {
		var zeroTime time.Time
		result := printer.FormatTimestamp(zeroTime, "15:04:05")
		assert.Equal(t, "N/A", result)
	})

	t.Run("returns N/A for time.Time{}", func(t *testing.T) {
		result := printer.FormatTimestamp(time.Time{}, "2006-01-02 15:04:05")
		assert.Equal(t, "N/A", result)
	})

	t.Run("formats with different layouts", func(t *testing.T) {
		ts := time.Date(2025, 12, 17, 10, 30, 45, 0, time.Local)
		assert.Equal(t, "2025-12-17", printer.FormatTimestamp(ts, "2006-01-02"))
		assert.Equal(t, "10:30", printer.FormatTimestamp(ts, "15:04"))
		assert.Equal(t, "Dec 17 10:30:45", printer.FormatTimestamp(ts, "Jan 02 15:04:05"))
	})
}
