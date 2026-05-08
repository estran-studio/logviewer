package opensearch

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
)

func TestBody(t *testing.T) {

	logSearch := client.LogSearch{
		Fields: map[string]string{
			"instance":        "pod-1234",
			"applicationName": "mfx.services.tsapi",
		},
		Range: client.SearchRange{Last: ty.OptWrap("30m")},
		Size:  ty.OptWrap(100),
	}

	request, err := GetSearchRequest(&logSearch)
	if err != nil {
		t.Error(err)
	}

	b, _ := json.MarshalIndent(&request, "", "    ")

	fmt.Println(string(b))
}

func TestGetSearchRequest_Pagination(t *testing.T) {
	t.Run("no page token", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
		}
		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if request.From != 0 {
			t.Errorf("expected From to be 0, but got %d", request.From)
		}
	})

	t.Run("with page token", func(t *testing.T) {
		logSearch := &client.LogSearch{
			PageToken: ty.Opt[string]{Value: "50", Set: true},
			Range:     client.SearchRange{Last: ty.OptWrap("30m")},
		}
		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if request.From != 50 {
			t.Errorf("expected From to be 50, but got %d", request.From)
		}
	})

	t.Run("with invalid page token", func(t *testing.T) {
		logSearch := &client.LogSearch{
			PageToken: ty.Opt[string]{Value: "invalid", Set: true},
			Range:     client.SearchRange{Last: ty.OptWrap("30m")},
		}
		_, err := GetSearchRequest(logSearch)
		if err == nil {
			t.Errorf("expected error for invalid page token, got nil")
		}
	})
}

func TestGetSearchRequest_RecursiveFilter(t *testing.T) {
	t.Run("simple AND filter", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicAnd,
				Filters: []client.Filter{
					{Field: "app", Value: "myapp"},
					{Field: "env", Value: "prod"},
				},
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		// Should contain bool must with the conditions
		if !strings.Contains(queryStr, "must") {
			t.Errorf("expected query to contain 'must', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "myapp") {
			t.Errorf("expected query to contain 'myapp', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "prod") {
			t.Errorf("expected query to contain 'prod', got: %s", queryStr)
		}
	})

	t.Run("simple OR filter", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicOr,
				Filters: []client.Filter{
					{Field: "level", Value: "ERROR"},
					{Field: "level", Value: "WARN"},
				},
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		// Should contain bool should with minimum_should_match
		if !strings.Contains(queryStr, "should") {
			t.Errorf("expected query to contain 'should', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "minimum_should_match") {
			t.Errorf("expected query to contain 'minimum_should_match', got: %s", queryStr)
		}
	})

	t.Run("NOT filter", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicNot,
				Filters: []client.Filter{
					{Field: "level", Value: "DEBUG"},
				},
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		// Should contain bool must_not
		if !strings.Contains(queryStr, "must_not") {
			t.Errorf("expected query to contain 'must_not', got: %s", queryStr)
		}
	})

	t.Run("nested (A OR B) AND C", func(t *testing.T) {
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
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		// Should contain nested structure
		if !strings.Contains(queryStr, "should") {
			t.Errorf("expected query to contain 'should' for OR, got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "myapp") {
			t.Errorf("expected query to contain 'myapp', got: %s", queryStr)
		}
	})

	t.Run("combined legacy fields and new filter", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Fields: map[string]string{
				"env": "production",
			},
			Filter: &client.Filter{
				Logic: client.LogicOr,
				Filters: []client.Filter{
					{Field: "level", Value: "ERROR"},
					{Field: "level", Value: "WARN"},
				},
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		// Should contain both legacy and new filter conditions
		if !strings.Contains(queryStr, "production") {
			t.Errorf("expected query to contain 'production', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "should") {
			t.Errorf("expected query to contain 'should', got: %s", queryStr)
		}
	})
}

// Tests for hl-compatible query operators
//
//nolint:gocyclo // Comprehensive test suite with many subtests
func TestGetSearchRequest_HLCompatibleOperators(t *testing.T) {
	t.Run("comparison operator - greater than", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field: "latency_ms",
				Op:    "gt",
				Value: "1000",
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		if !strings.Contains(queryStr, "range") {
			t.Errorf("expected query to contain 'range', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, `"gt"`) {
			t.Errorf("expected query to contain 'gt', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "1000") {
			t.Errorf("expected query to contain '1000', got: %s", queryStr)
		}
	})

	t.Run("comparison operator - greater than or equal", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field: "latency_ms",
				Op:    "gte",
				Value: "500",
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		if !strings.Contains(queryStr, "range") {
			t.Errorf("expected query to contain 'range', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, `"gte"`) {
			t.Errorf("expected query to contain 'gte', got: %s", queryStr)
		}
	})

	t.Run("comparison operator - less than", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field: "latency_ms",
				Op:    "lt",
				Value: "100",
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		if !strings.Contains(queryStr, "range") {
			t.Errorf("expected query to contain 'range', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, `"lt"`) {
			t.Errorf("expected query to contain 'lt', got: %s", queryStr)
		}
	})

	t.Run("comparison operator - less than or equal", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field: "latency_ms",
				Op:    "lte",
				Value: "200",
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		if !strings.Contains(queryStr, "range") {
			t.Errorf("expected query to contain 'range', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, `"lte"`) {
			t.Errorf("expected query to contain 'lte', got: %s", queryStr)
		}
	})

	t.Run("negate field - equals with negate", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field:  "level",
				Op:     "equals",
				Value:  "INFO",
				Negate: true,
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		if !strings.Contains(queryStr, "must_not") {
			t.Errorf("expected query to contain 'must_not' for negated filter, got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "INFO") {
			t.Errorf("expected query to contain 'INFO', got: %s", queryStr)
		}
	})

	t.Run("negate field - comparison with negate", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Field:  "latency_ms",
				Op:     "gt",
				Value:  "1000",
				Negate: true,
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		if !strings.Contains(queryStr, "must_not") {
			t.Errorf("expected query to contain 'must_not' for negated filter, got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "range") {
			t.Errorf("expected query to contain 'range', got: %s", queryStr)
		}
	})

	t.Run("complex - OR with comparison operators", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicOr,
				Filters: []client.Filter{
					{Field: "level", Value: "ERROR"},
					{Field: "latency_ms", Op: "gt", Value: "1000"},
				},
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		if !strings.Contains(queryStr, "should") {
			t.Errorf("expected query to contain 'should' for OR, got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "ERROR") {
			t.Errorf("expected query to contain 'ERROR', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "range") {
			t.Errorf("expected query to contain 'range', got: %s", queryStr)
		}
	})

	t.Run("complex - AND with negation", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicAnd,
				Filters: []client.Filter{
					{Field: "level", Value: "ERROR"},
					{Field: "app", Op: "equals", Value: "payment-service", Negate: true},
				},
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		if !strings.Contains(queryStr, "must") {
			t.Errorf("expected query to contain 'must' for AND, got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "must_not") {
			t.Errorf("expected query to contain 'must_not' for negated filter, got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "ERROR") {
			t.Errorf("expected query to contain 'ERROR', got: %s", queryStr)
		}
	})

	t.Run("comparison range - latency between values", func(t *testing.T) {
		logSearch := &client.LogSearch{
			Filter: &client.Filter{
				Logic: client.LogicAnd,
				Filters: []client.Filter{
					{Field: "latency_ms", Op: "gte", Value: "500"},
					{Field: "latency_ms", Op: "lt", Value: "2000"},
				},
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		if !strings.Contains(queryStr, `"gte"`) {
			t.Errorf("expected query to contain 'gte', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, `"lt"`) {
			t.Errorf("expected query to contain 'lt', got: %s", queryStr)
		}
	})
}

func TestGetSearchRequest_NativeQuery(t *testing.T) {
	t.Run("native query standalone", func(t *testing.T) {
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(`status:500 AND host:server*`),
			Range:       client.SearchRange{Last: ty.OptWrap("30m")},
			Size:        ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		if !strings.Contains(queryStr, "query_string") {
			t.Errorf("expected query to contain 'query_string', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "status:500 AND host:server*") {
			t.Errorf("expected query to contain native query, got: %s", queryStr)
		}
	})

	t.Run("native query with filters appended", func(t *testing.T) {
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(`message:error*`),
			Filter: &client.Filter{
				Field: "level",
				Value: "ERROR",
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		// Should contain both native query and filter
		if !strings.Contains(queryStr, "query_string") {
			t.Errorf("expected query to contain 'query_string', got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "message:error*") {
			t.Errorf("expected query to contain native query, got: %s", queryStr)
		}
		if !strings.Contains(queryStr, "ERROR") {
			t.Errorf("expected query to contain filter value 'ERROR', got: %s", queryStr)
		}
	})

	t.Run("empty native query is ignored", func(t *testing.T) {
		logSearch := &client.LogSearch{
			NativeQuery: ty.OptWrap(""),
			Filter: &client.Filter{
				Field: "level",
				Value: "ERROR",
			},
			Range: client.SearchRange{Last: ty.OptWrap("30m")},
			Size:  ty.OptWrap(100),
		}

		request, err := GetSearchRequest(logSearch)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		b, _ := json.MarshalIndent(&request, "", "    ")
		queryStr := string(b)

		// Should NOT contain query_string since native query is empty
		if strings.Contains(queryStr, "query_string") {
			t.Errorf("expected query to NOT contain 'query_string' for empty native query, got: %s", queryStr)
		}
		// Should still contain the filter
		if !strings.Contains(queryStr, "ERROR") {
			t.Errorf("expected query to contain filter value 'ERROR', got: %s", queryStr)
		}
	})
}
