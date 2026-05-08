package query_test

import (
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
	"github.com/estran-studio/logviewer/pkg/query"
)

func TestIsHLSyntax(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		expected bool
	}{
		{"legacy equals", "level=error", false},
		{"hl not equals", "level!=error", true},
		{"hl regex", "message~=error.*", true},
		{"hl not regex", "message!~=error.*", true},
		{"hl greater than", "status>400", true},
		{"hl greater or equal", "status>=400", true},
		{"hl less than", "duration<1.5", true},
		{"hl less or equal", "duration<=1.5", true},
		{"no operator", "level", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := query.IsHLSyntax(tt.expr)
			if result != tt.expected {
				t.Errorf("IsHLSyntax(%q) = %v, want %v", tt.expr, result, tt.expected)
			}
		})
	}
}

func TestParseFilterFlag(t *testing.T) {
	tests := []struct {
		name        string
		expr        string
		expected    *client.Filter
		expectError bool
	}{
		{
			name: "equals",
			expr: "level=error",
			expected: &client.Filter{
				Field:  "level",
				Op:     operator.Equals,
				Value:  "error",
				Negate: false,
			},
		},
		{
			name: "not equals",
			expr: "level!=debug",
			expected: &client.Filter{
				Field:  "level",
				Op:     operator.Equals,
				Value:  "debug",
				Negate: true,
			},
		},
		{
			name: "regex",
			expr: "message~=error.*timeout",
			expected: &client.Filter{
				Field:  "message",
				Op:     operator.Regex,
				Value:  "error.*timeout",
				Negate: false,
			},
		},
		{
			name: "not regex",
			expr: "message!~=debug.*",
			expected: &client.Filter{
				Field:  "message",
				Op:     operator.Regex,
				Value:  "debug.*",
				Negate: true,
			},
		},
		{
			name: "greater than",
			expr: "status>400",
			expected: &client.Filter{
				Field:  "status",
				Op:     operator.Gt,
				Value:  "400",
				Negate: false,
			},
		},
		{
			name: "greater or equal",
			expr: "status>=400",
			expected: &client.Filter{
				Field:  "status",
				Op:     operator.Gte,
				Value:  "400",
				Negate: false,
			},
		},
		{
			name: "less than",
			expr: "duration<0.5",
			expected: &client.Filter{
				Field:  "duration",
				Op:     operator.Lt,
				Value:  "0.5",
				Negate: false,
			},
		},
		{
			name: "less or equal",
			expr: "duration<=1.5",
			expected: &client.Filter{
				Field:  "duration",
				Op:     operator.Lte,
				Value:  "1.5",
				Negate: false,
			},
		},
		{
			name: "value with quotes",
			expr: `service="my-api"`,
			expected: &client.Filter{
				Field:  "service",
				Op:     operator.Equals,
				Value:  "my-api",
				Negate: false,
			},
		},
		{
			name: "value with single quotes",
			expr: `service='my-api'`,
			expected: &client.Filter{
				Field:  "service",
				Op:     operator.Equals,
				Value:  "my-api",
				Negate: false,
			},
		},
		{
			name: "field with spaces trimmed",
			expr: " level = error ",
			expected: &client.Filter{
				Field:  "level",
				Op:     operator.Equals,
				Value:  "error",
				Negate: false,
			},
		},
		{
			name:        "empty expression",
			expr:        "",
			expectError: true,
		},
		{
			name:        "no operator",
			expr:        "level",
			expectError: true,
		},
		{
			name:        "missing field",
			expr:        "=value",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := query.ParseFilterFlag(tt.expr)

			if tt.expectError {
				if err == nil {
					t.Errorf("ParseFilterFlag(%q) expected error, got nil", tt.expr)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseFilterFlag(%q) unexpected error: %v", tt.expr, err)
				return
			}

			if result.Field != tt.expected.Field {
				t.Errorf("Field = %q, want %q", result.Field, tt.expected.Field)
			}
			if result.Op != tt.expected.Op {
				t.Errorf("Op = %q, want %q", result.Op, tt.expected.Op)
			}
			if result.Value != tt.expected.Value {
				t.Errorf("Value = %q, want %q", result.Value, tt.expected.Value)
			}
			if result.Negate != tt.expected.Negate {
				t.Errorf("Negate = %v, want %v", result.Negate, tt.expected.Negate)
			}
		})
	}
}

func TestParseFilterFlags(t *testing.T) {
	tests := []struct {
		name          string
		exprs         []string
		expectedCount int
		expectedLogic client.LogicOperator
		expectError   bool
	}{
		{
			name:          "empty",
			exprs:         []string{},
			expectedCount: 0,
		},
		{
			name:          "single filter",
			exprs:         []string{"level=error"},
			expectedCount: 1,
			expectedLogic: "", // No logic for single filter
		},
		{
			name:          "multiple filters",
			exprs:         []string{"level=error", "status>=400"},
			expectedCount: 2,
			expectedLogic: client.LogicAnd,
		},
		{
			name:        "invalid filter",
			exprs:       []string{"level=error", "invalid"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := query.ParseFilterFlags(tt.exprs)

			if tt.expectError {
				if err == nil {
					t.Errorf("ParseFilterFlags expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseFilterFlags unexpected error: %v", err)
				return
			}

			if tt.expectedCount == 0 {
				if result != nil {
					t.Errorf("expected nil result for empty input")
				}
				return
			}

			if result == nil {
				t.Fatalf("expected non-nil result")
			}

			if tt.expectedCount == 1 {
				// Single filter, no group
				if result.Logic != "" {
					t.Errorf("expected no logic for single filter, got %q", result.Logic)
				}
			} else {
				// Multiple filters, should be AND group
				if result.Logic != tt.expectedLogic {
					t.Errorf("Logic = %q, want %q", result.Logic, tt.expectedLogic)
				}
				if len(result.Filters) != tt.expectedCount {
					t.Errorf("got %d filters, want %d", len(result.Filters), tt.expectedCount)
				}
			}
		})
	}
}

func TestParseLegacyFilter(t *testing.T) {
	tests := []struct {
		name        string
		fieldExpr   string
		opExpr      string
		expected    *client.Filter
		expectError bool
	}{
		{
			name:      "simple equals",
			fieldExpr: "level=error",
			opExpr:    "",
			expected: &client.Filter{
				Field: "level",
				Op:    operator.Equals,
				Value: "error",
			},
		},
		{
			name:      "with match operator",
			fieldExpr: "message=timeout",
			opExpr:    "match",
			expected: &client.Filter{
				Field: "message",
				Op:    operator.Match,
				Value: "timeout",
			},
		},
		{
			name:      "with regex operator",
			fieldExpr: "path=/api/.*",
			opExpr:    "regex",
			expected: &client.Filter{
				Field: "path",
				Op:    operator.Regex,
				Value: "/api/.*",
			},
		},
		{
			name:        "missing equals",
			fieldExpr:   "level",
			opExpr:      "",
			expectError: true,
		},
		{
			name:        "empty expression",
			fieldExpr:   "",
			opExpr:      "",
			expectError: true,
		},
		{
			name:        "invalid operator",
			fieldExpr:   "level=error",
			opExpr:      "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := query.ParseLegacyFilter(tt.fieldExpr, tt.opExpr)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.Field != tt.expected.Field {
				t.Errorf("Field = %q, want %q", result.Field, tt.expected.Field)
			}
			if result.Op != tt.expected.Op {
				t.Errorf("Op = %q, want %q", result.Op, tt.expected.Op)
			}
			if result.Value != tt.expected.Value {
				t.Errorf("Value = %q, want %q", result.Value, tt.expected.Value)
			}
		})
	}
}
