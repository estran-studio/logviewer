package query_test

import (
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
	"github.com/estran-studio/logviewer/pkg/query"
)

func TestLexer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []query.TokenType
	}{
		{
			name:     "simple condition",
			input:    "level=error",
			expected: []query.TokenType{query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "condition with spaces",
			input:    "level = error",
			expected: []query.TokenType{query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "AND expression",
			input:    "level=error AND status>=400",
			expected: []query.TokenType{query.TokenField, query.TokenOperator, query.TokenValue, query.TokenAnd, query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "OR expression",
			input:    "level=error OR level=warn",
			expected: []query.TokenType{query.TokenField, query.TokenOperator, query.TokenValue, query.TokenOr, query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "NOT expression",
			input:    "NOT level=debug",
			expected: []query.TokenType{query.TokenNot, query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "parentheses",
			input:    "(level=error)",
			expected: []query.TokenType{query.TokenLParen, query.TokenField, query.TokenOperator, query.TokenValue, query.TokenRParen, query.TokenEOF},
		},
		{
			name:     "complex with parentheses",
			input:    "(level=error OR status>=500) AND service=api",
			expected: []query.TokenType{query.TokenLParen, query.TokenField, query.TokenOperator, query.TokenValue, query.TokenOr, query.TokenField, query.TokenOperator, query.TokenValue, query.TokenRParen, query.TokenAnd, query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "exists function",
			input:    "exists(error)",
			expected: []query.TokenType{query.TokenExists, query.TokenLParen, query.TokenField, query.TokenRParen, query.TokenEOF},
		},
		{
			name:     "quoted value",
			input:    `service="my-api"`,
			expected: []query.TokenType{query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "symbolic AND",
			input:    "level=error && status>=400",
			expected: []query.TokenType{query.TokenField, query.TokenOperator, query.TokenValue, query.TokenAnd, query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "symbolic OR",
			input:    "level=error || level=warn",
			expected: []query.TokenType{query.TokenField, query.TokenOperator, query.TokenValue, query.TokenOr, query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "CONTAINS operator",
			input:    "message CONTAINS error",
			expected: []query.TokenType{query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "LIKE operator",
			input:    "path LIKE /api/%",
			expected: []query.TokenType{query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "CONTAINS with quoted value",
			input:    `message CONTAINS "connection refused"`,
			expected: []query.TokenType{query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
		{
			name:     "LIKE in complex expression",
			input:    "level=error AND path LIKE /api/%",
			expected: []query.TokenType{query.TokenField, query.TokenOperator, query.TokenValue, query.TokenAnd, query.TokenField, query.TokenOperator, query.TokenValue, query.TokenEOF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := query.NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("Tokenize error: %v", err)
			}

			if len(tokens) != len(tt.expected) {
				t.Fatalf("expected %d tokens, got %d: %+v", len(tt.expected), len(tokens), tokens)
			}

			for i, expected := range tt.expected {
				if tokens[i].Type != expected {
					t.Errorf("token %d: expected type %d, got %d (value: %s)", i, expected, tokens[i].Type, tokens[i].Value)
				}
			}
		})
	}
}

// TestLexer_OperatorTranslation tests that keyword operators are properly translated
func TestLexer_OperatorTranslation(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedOperator string
	}{
		{
			name:             "CONTAINS maps to ~=",
			input:            "field CONTAINS value",
			expectedOperator: "~=",
		},
		{
			name:             "contains (lowercase) maps to ~=",
			input:            "field contains value",
			expectedOperator: "~=",
		},
		{
			name:             "LIKE maps to like",
			input:            "field LIKE pattern",
			expectedOperator: "like",
		},
		{
			name:             "like (lowercase) maps to like",
			input:            "field like pattern",
			expectedOperator: "like",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := query.NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("Tokenize error: %v", err)
			}

			// Find the operator token (second token should be operator)
			if len(tokens) < 3 {
				t.Fatalf("Expected at least 3 tokens, got %d", len(tokens))
			}

			if tokens[1].Type != query.TokenOperator {
				t.Fatalf("Second token should be operator, got %v", tokens[1].Type)
			}

			if tokens[1].Value != tt.expectedOperator {
				t.Errorf("Expected operator '%s', got '%s'", tt.expectedOperator, tokens[1].Value)
			}
		})
	}
}

//nolint:gocyclo // Comprehensive test suite with many test cases
func TestParseQueryExpression(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		validate    func(t *testing.T, f *client.Filter)
		expectError bool
	}{
		{
			name:  "simple equals",
			input: "level=error",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Field != "level" {
					t.Errorf("Field = %q, want 'level'", f.Field)
				}
				if f.Op != operator.Equals {
					t.Errorf("Op = %q, want 'equals'", f.Op)
				}
				if f.Value != "error" {
					t.Errorf("Value = %q, want 'error'", f.Value)
				}
			},
		},
		{
			name:  "not equals",
			input: "level!=debug",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Field != "level" {
					t.Errorf("Field = %q, want 'level'", f.Field)
				}
				if f.Op != operator.Equals {
					t.Errorf("Op = %q, want 'equals'", f.Op)
				}
				if !f.Negate {
					t.Error("Negate should be true")
				}
			},
		},
		{
			name:  "greater than",
			input: "status>400",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Op != operator.Gt {
					t.Errorf("Op = %q, want 'gt'", f.Op)
				}
				if f.Value != "400" {
					t.Errorf("Value = %q, want '400'", f.Value)
				}
			},
		},
		{
			name:  "simple AND",
			input: "level=error AND status>=400",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Logic != client.LogicAnd {
					t.Errorf("Logic = %q, want 'AND'", f.Logic)
				}
				if len(f.Filters) != 2 {
					t.Errorf("expected 2 filters, got %d", len(f.Filters))
				}
			},
		},
		{
			name:  "simple OR",
			input: "level=error OR level=warn",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Logic != client.LogicOr {
					t.Errorf("Logic = %q, want 'OR'", f.Logic)
				}
				if len(f.Filters) != 2 {
					t.Errorf("expected 2 filters, got %d", len(f.Filters))
				}
			},
		},
		{
			name:  "NOT expression",
			input: "NOT level=debug",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Logic != client.LogicNot {
					t.Errorf("Logic = %q, want 'NOT'", f.Logic)
				}
				if len(f.Filters) != 1 {
					t.Errorf("expected 1 filter, got %d", len(f.Filters))
				}
			},
		},
		{
			name:  "parenthesized expression",
			input: "(level=error OR status>=500) AND service=api",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Logic != client.LogicAnd {
					t.Errorf("Logic = %q, want 'AND'", f.Logic)
				}
				if len(f.Filters) != 2 {
					t.Errorf("expected 2 filters, got %d", len(f.Filters))
				}
				// First filter should be OR group
				if f.Filters[0].Logic != client.LogicOr {
					t.Errorf("First filter Logic = %q, want 'OR'", f.Filters[0].Logic)
				}
				// Second filter should be leaf
				if f.Filters[1].Field != "service" {
					t.Errorf("Second filter Field = %q, want 'service'", f.Filters[1].Field)
				}
			},
		},
		{
			name:  "exists function",
			input: "exists(error_code)",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Field != "error_code" {
					t.Errorf("Field = %q, want 'error_code'", f.Field)
				}
				if f.Op != operator.Exists {
					t.Errorf("Op = %q, want 'exists'", f.Op)
				}
			},
		},
		{
			name:  "exists with AND",
			input: "exists(error) AND level=error",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Logic != client.LogicAnd {
					t.Errorf("Logic = %q, want 'AND'", f.Logic)
				}
				if f.Filters[0].Op != operator.Exists {
					t.Errorf("First filter Op = %q, want 'exists'", f.Filters[0].Op)
				}
			},
		},
		{
			name:  "regex operator",
			input: "message~=error.*timeout",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Op != operator.Regex {
					t.Errorf("Op = %q, want 'regex'", f.Op)
				}
				if f.Negate {
					t.Error("Negate should be false")
				}
			},
		},
		{
			name:  "not regex operator",
			input: "message!~=debug.*",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Op != operator.Regex {
					t.Errorf("Op = %q, want 'regex'", f.Op)
				}
				if !f.Negate {
					t.Error("Negate should be true")
				}
			},
		},
		{
			name:  "quoted value with spaces",
			input: `service="my cool api"`,
			validate: func(t *testing.T, f *client.Filter) {
				if f.Value != "my cool api" {
					t.Errorf("Value = %q, want 'my cool api'", f.Value)
				}
			},
		},
		{
			name:  "symbolic AND",
			input: "level=error && status>=400",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Logic != client.LogicAnd {
					t.Errorf("Logic = %q, want 'AND'", f.Logic)
				}
			},
		},
		{
			name:  "symbolic OR",
			input: "level=error || level=warn",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Logic != client.LogicOr {
					t.Errorf("Logic = %q, want 'OR'", f.Logic)
				}
			},
		},
		{
			name:  "empty expression",
			input: "",
			validate: func(t *testing.T, f *client.Filter) {
				if f != nil {
					t.Error("expected nil filter for empty input")
				}
			},
		},
		{
			name:  "complex nested expression",
			input: "(level=error AND status>=500) OR (level=warn AND duration>1000)",
			validate: func(t *testing.T, f *client.Filter) {
				if f.Logic != client.LogicOr {
					t.Errorf("Logic = %q, want 'OR'", f.Logic)
				}
				if len(f.Filters) != 2 {
					t.Errorf("expected 2 filters, got %d", len(f.Filters))
				}
				// Both children should be AND groups
				if f.Filters[0].Logic != client.LogicAnd {
					t.Errorf("First filter Logic = %q, want 'AND'", f.Filters[0].Logic)
				}
				if f.Filters[1].Logic != client.LogicAnd {
					t.Errorf("Second filter Logic = %q, want 'AND'", f.Filters[1].Logic)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := query.ParseQueryExpression(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			tt.validate(t, result)
		})
	}
}

func TestParseQueryExpression_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "missing value",
			input: "level=",
		},
		{
			name:  "missing operator",
			input: "level error",
		},
		{
			name:  "unclosed parenthesis",
			input: "(level=error",
		},
		{
			name:  "unterminated quote",
			input: `service="my api`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := query.ParseQueryExpression(tt.input)
			if err == nil {
				t.Errorf("expected error for input %q", tt.input)
			}
		})
	}
}
