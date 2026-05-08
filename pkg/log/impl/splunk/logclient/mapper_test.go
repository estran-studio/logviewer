package logclient

import (
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

func TestSearchRequest(t *testing.T) {

	t.Run("simple search with index and one equals condition", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields:          ty.MS{"application_name": "wq.services.pet"},
			FieldsCondition: ty.MS{},
			Options:         ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=nonprod application_name="wq.services.pet"`, requestBodyFields["search"])
	})

	t.Run("free text search token", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields:          ty.MS{"_": "error occurred"},
			FieldsCondition: ty.MS{"_": ""},
			Options:         ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		// should include index and quoted phrase after it
		assert.Equal(t, `index=nonprod "error occurred"`, requestBodyFields["search"])
	})

	t.Run("search with multiple equals conditions", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields:          ty.MS{"application_name": "wq.services.pet", "trace_id": "1234"},
			FieldsCondition: ty.MS{},
			Options:         ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `index=nonprod`)
		assert.Contains(t, requestBodyFields["search"], `application_name="wq.services.pet"`)
		assert.Contains(t, requestBodyFields["search"], `trace_id="1234"`)
	})

	t.Run("search with wildcard condition", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields:          ty.MS{"application_name": "wq.services"},
			FieldsCondition: ty.MS{"application_name": operator.Wildcard},
			Options:         ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=nonprod application_name="wq.services*"`, requestBodyFields["search"])
	})

	t.Run("search with wildcard condition and spaces", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields:          ty.MS{"application_name": "wq services"},
			FieldsCondition: ty.MS{"application_name": operator.Wildcard},
			Options:         ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=nonprod application_name="wq services*"`, requestBodyFields["search"])
	})

	t.Run("search with exists condition", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields:          ty.MS{"trace_id": ""},
			FieldsCondition: ty.MS{"trace_id": operator.Exists},
			Options:         ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=nonprod trace_id=*`, requestBodyFields["search"])
	})

	t.Run("search with regex condition", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields:          ty.MS{"message": "(error|fail)"},
			FieldsCondition: ty.MS{"message": operator.Regex},
			Options:         ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=nonprod | regex message="(error|fail)"`, requestBodyFields["search"])
	})

	t.Run("complex search with multiple operators", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields: ty.MS{
				"application_name": "wq.services.pet",
				"http_method":      "GET",
				"message":          "(error|fail)",
				"trace_id":         "",
			},
			FieldsCondition: ty.MS{
				"application_name": operator.Wildcard,
				"http_method":      operator.Equals,
				"message":          operator.Regex,
				"trace_id":         operator.Exists,
			},
			Options: ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `index=nonprod`)
		assert.Contains(t, requestBodyFields["search"], `application_name="wq.services.pet*"`)
		assert.Contains(t, requestBodyFields["search"], `http_method="GET"`)
		assert.Contains(t, requestBodyFields["search"], `trace_id=*`)
		assert.Contains(t, requestBodyFields["search"], `| regex message="(error|fail)"`)
	})

	t.Run("search with value containing spaces", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields:          ty.MS{"message": "this is a test"},
			FieldsCondition: ty.MS{},
			Options:         ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=nonprod message="this is a test"`, requestBodyFields["search"])
	})

	t.Run("search with value containing double quotes", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields:          ty.MS{"message": `this is a "test"`},
			FieldsCondition: ty.MS{},
			Options:         ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=nonprod message="this is a \"test\""`, requestBodyFields["search"])
	})

	t.Run("use last duration instead of explicit times", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields:          ty.MS{"_": "hello"},
			FieldsCondition: ty.MS{},
			Options:         ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Last.S("1min")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, "-1min", requestBodyFields["earliest_time"])
		assert.Equal(t, "now", requestBodyFields["latest_time"])
	})

	// Tests for new recursive Filter AST
	t.Run("recursive filter - simple AND", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicAnd,
				Filters: []client.Filter{
					{Field: "app", Value: "myapp"},
					{Field: "env", Value: "prod"},
				},
			},
			Options: ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `app="myapp"`)
		assert.Contains(t, requestBodyFields["search"], `env="prod"`)
	})

	t.Run("recursive filter - simple OR", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicOr,
				Filters: []client.Filter{
					{Field: "level", Value: "ERROR"},
					{Field: "level", Value: "WARN"},
				},
			},
			Options: ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `level="ERROR" OR level="WARN"`)
	})

	t.Run("recursive filter - NOT", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicNot,
				Filters: []client.Filter{
					{Field: "level", Value: "DEBUG"},
				},
			},
			Options: ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `NOT (level="DEBUG")`)
	})

	t.Run("recursive filter - nested (A OR B) AND C", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicAnd,
				Filters: []client.Filter{
					{
						Logic: client.LogicOr,
						Filters: []client.Filter{
							{Field: "level", Value: "ERROR"},
							{Field: "level", Value: "WARN"},
						},
					},
					{Field: "app", Value: "myapp"},
				},
			},
			Options: ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `(level="ERROR" OR level="WARN")`)
		assert.Contains(t, requestBodyFields["search"], `app="myapp"`)
	})

	t.Run("recursive filter - regex in filter", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicAnd,
				Filters: []client.Filter{
					{Field: "app", Value: "myapp"},
					{Field: "message", Op: operator.Regex, Value: "(error|fail)"},
				},
			},
			Options: ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `app="myapp"`)
		assert.Contains(t, requestBodyFields["search"], `| regex message="(error|fail)"`)
	})

	t.Run("recursive filter - combined with legacy fields", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields: ty.MS{"env": "production"},
			Filter: &client.Filter{
				Logic: client.LogicOr,
				Filters: []client.Filter{
					{Field: "level", Value: "ERROR"},
					{Field: "level", Value: "WARN"},
				},
			},
			Options: ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `env="production"`)
		assert.Contains(t, requestBodyFields["search"], `level="ERROR" OR level="WARN"`)
	})

	t.Run("recursive filter - wildcard operator", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field: "app",
				Op:    operator.Wildcard,
				Value: "wq.services",
			},
			Options: ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `app="wq.services*"`)
	})

	t.Run("recursive filter - exists operator", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field: "trace_id",
				Op:    operator.Exists,
			},
			Options: ty.MI{"index": "nonprod"},
		}
		logSearch.Range.Gte.S("24h@h")
		logSearch.Range.Lte.S("now")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `trace_id=*`)
	})

	// Tests for NativeQuery support
	t.Run("native query - standalone", func(t *testing.T) {
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(`index=main sourcetype=syslog | stats count by host`),
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=main sourcetype=syslog | stats count by host`, requestBodyFields["search"])
	})

	t.Run("native query - options.index is ignored when nativeQuery is set", func(t *testing.T) {
		// When nativeQuery is provided, the user has full control over the index.
		// options.index should NOT be appended to avoid redundant/conflicting indices.
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(`index=main sourcetype=access_combined | top limit=10 uri`),
			Options:     ty.MI{"index": "web"}, // This should be ignored
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=main sourcetype=access_combined | top limit=10 uri`, requestBodyFields["search"])
		assert.NotContains(t, requestBodyFields["search"], `index=web`)
	})

	t.Run("native query - with filters appended", func(t *testing.T) {
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(`index=main sourcetype=syslog`),
			Fields:      ty.MS{"host": "server01"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=main sourcetype=syslog | search host="server01"`, requestBodyFields["search"])
	})

	t.Run("native query - with filters appended as single search command", func(t *testing.T) {
		// All filters should be combined into a single | search command
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(`index=main sourcetype=access_combined`),
			Options:     ty.MI{"index": "web"}, // Should be ignored
			Filter: &client.Filter{
				Logic: client.LogicOr,
				Filters: []client.Filter{
					{Field: "status", Value: "500"},
					{Field: "status", Value: "503"},
				},
			},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		// Should have native query followed by single | search with filters
		assert.Equal(t, `index=main sourcetype=access_combined | search (status="500" OR status="503")`, requestBodyFields["search"])
		// Should NOT have multiple | search commands or index=web
		assert.NotContains(t, requestBodyFields["search"], `index=web`)
	})

	t.Run("native query - empty value is ignored", func(t *testing.T) {
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(""),
			Options:     ty.MI{"index": "main"},
			Fields:      ty.MS{"level": "ERROR"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=main level="ERROR"`, requestBodyFields["search"])
	})

	// Tests for trailing pipe handling
	t.Run("native query - trailing pipe is trimmed", func(t *testing.T) {
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(`index=main sourcetype=syslog |`),
			Fields:      ty.MS{"level": "ERROR"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		// Should NOT produce "... | | search ..." - trailing pipe should be trimmed
		assert.Equal(t, `index=main sourcetype=syslog | search level="ERROR"`, requestBodyFields["search"])
		assert.NotContains(t, requestBodyFields["search"], "| |")
	})

	t.Run("native query - trailing pipe with whitespace is trimmed", func(t *testing.T) {
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(`index=main sourcetype=syslog |   `),
			Fields:      ty.MS{"host": "server01"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=main sourcetype=syslog | search host="server01"`, requestBodyFields["search"])
	})

	t.Run("native query - multiple trailing pipes are trimmed", func(t *testing.T) {
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(`index=main | where status>400 | |  `),
			Fields:      ty.MS{"app": "web"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Equal(t, `index=main | where status>400 | search app="web"`, requestBodyFields["search"])
	})

	t.Run("native query - no filters does not add spurious search command", func(t *testing.T) {
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(`index=main | stats count by host |`),
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		// Should just trim the trailing pipe, not add anything
		assert.Equal(t, `index=main | stats count by host`, requestBodyFields["search"])
	})
}

func TestTrimTrailingPipe(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no trailing pipe", "index=main | stats count", "index=main | stats count"},
		{"single trailing pipe", "index=main |", "index=main"},
		{"trailing pipe with space", "index=main | ", "index=main"},
		{"trailing pipe with multiple spaces", "index=main |   ", "index=main"},
		{"multiple trailing pipes", "index=main | |", "index=main"},
		{"multiple trailing pipes with spaces", "index=main |  |  ", "index=main"},
		{"trailing whitespace only", "index=main   ", "index=main"},
		{"empty string", "", ""},
		{"pipe only", "|", ""},
		{"complex query with trailing pipe", "index=main | where x>1 | eval y=2 |", "index=main | where x>1 | eval y=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := trimTrailingPipe(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Tests for hl-compatible query operators
func TestSearchRequest_HLCompatibleOperators(t *testing.T) {
	t.Run("comparison operator - greater than", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field: "latency_ms",
				Op:    operator.Gt,
				Value: "1000",
			},
			Options: ty.MI{"index": "main"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `latency_ms>1000`)
	})

	t.Run("comparison operator - greater than or equal", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field: "latency_ms",
				Op:    operator.Gte,
				Value: "500",
			},
			Options: ty.MI{"index": "main"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `latency_ms>=500`)
	})

	t.Run("comparison operator - less than", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field: "latency_ms",
				Op:    operator.Lt,
				Value: "100",
			},
			Options: ty.MI{"index": "main"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `latency_ms<100`)
	})

	t.Run("comparison operator - less than or equal", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field: "latency_ms",
				Op:    operator.Lte,
				Value: "200",
			},
			Options: ty.MI{"index": "main"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `latency_ms<=200`)
	})

	t.Run("negate field - equals with negate", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field:  "level",
				Op:     operator.Equals,
				Value:  "DEBUG",
				Negate: true,
			},
			Options: ty.MI{"index": "main"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `NOT`)
		assert.Contains(t, requestBodyFields["search"], `level="DEBUG"`)
	})

	t.Run("negate field - regex with negate", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field:  "message",
				Op:     operator.Regex,
				Value:  ".*success.*",
				Negate: true,
			},
			Options: ty.MI{"index": "main"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		// Negated regex in Splunk uses `where NOT match(...)` for valid SPL
		assert.Contains(t, requestBodyFields["search"], `where NOT match(message, ".*success.*")`)
	})

	t.Run("complex - OR with comparison operators", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicOr,
				Filters: []client.Filter{
					{Field: "level", Value: "ERROR"},
					{Field: "latency_ms", Op: operator.Gt, Value: "1000"},
				},
			},
			Options: ty.MI{"index": "main"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `level="ERROR"`)
		assert.Contains(t, requestBodyFields["search"], `latency_ms>1000`)
		assert.Contains(t, requestBodyFields["search"], `OR`)
	})

	t.Run("complex - AND with negation", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicAnd,
				Filters: []client.Filter{
					{Field: "level", Value: "ERROR"},
					{Field: "app", Op: operator.Equals, Value: "payment-service", Negate: true},
				},
			},
			Options: ty.MI{"index": "main"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `level="ERROR"`)
		assert.Contains(t, requestBodyFields["search"], `NOT`)
		assert.Contains(t, requestBodyFields["search"], `app="payment-service"`)
	})

	t.Run("comparison with range - latency between values", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicAnd,
				Filters: []client.Filter{
					{Field: "latency_ms", Op: operator.Gte, Value: "500"},
					{Field: "latency_ms", Op: operator.Lt, Value: "2000"},
				},
			},
			Options: ty.MI{"index": "main"},
		}
		logSearch.Range.Last.S("1h")

		requestBodyFields, err := getSearchRequest(logSearch)
		assert.NoError(t, err)
		assert.Contains(t, requestBodyFields["search"], `latency_ms>=500`)
		assert.Contains(t, requestBodyFields["search"], `latency_ms<2000`)
	})
}

func TestContainsTransformingCommand(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		// Transforming commands - should return true
		{"stats command", "index=main | stats count by host", true},
		{"chart command", "index=main | chart count over time", true},
		{"timechart command", "index=main | timechart span=1h count", true},
		{"top command", "index=main | top limit=10 uri", true},
		{"rare command", "index=main | rare src_ip", true},
		{"table command", "index=main | table host, source, _time", true},
		{"fields command without modifier", "index=main | fields host, source", true},
		{"fields - command", "index=main | fields - host, source", true},
		{"transaction command", "index=main | transaction session_id", true},
		{"tstats command", "| tstats count where index=main by host", true},
		{"eventstats command", "index=main | eventstats avg(duration) by host", true},
		{"streamstats command", "index=main | streamstats count by host", true},
		{"bucket command", "index=main | bucket _time span=1h", true},

		// Case insensitive
		{"stats uppercase", "index=main | STATS count by host", true},
		{"Stats mixed case", "index=main | Stats count by host", true},

		// Non-transforming commands - should return false
		{"simple search", "index=main sourcetype=syslog", false},
		{"search with where", "index=main | where status=500", false},
		{"search with eval", "index=main | eval severity=if(level==\"ERROR\", \"HIGH\", \"LOW\")", false},
		{"search with rex", "index=main | rex field=_raw \"(?<ip>\\d+\\.\\d+\\.\\d+\\.\\d+)\"", false},
		{"search with search", "index=main | search level=ERROR", false},
		{"search with head", "index=main | head 100", false},
		{"search with tail", "index=main | tail 100", false},
		{"search with sort", "index=main | sort -_time", false},
		{"search with dedup", "index=main | dedup host", false},
		{"fields + command (NOT transforming)", "index=main | fields + host, source, _time", false},
		{"fields + with application", "index=checkout-nonprod | fields + application_name, environment, region", false},

		// Edge cases
		{"empty query", "", false},
		{"no pipe", "index=main sourcetype=json level=ERROR", false},
		{"stats in field value", `index=main message="stats count"`, false},
		{"topaz should not match top", "index=main app=topaz", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsTransformingCommand(tt.query)
			assert.Equal(t, tt.expected, result, "query: %s", tt.query)
		})
	}
}
