package query

import (
	"fmt"
	"strings"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
)

// operatorMapping maps hl syntax operators to internal operators and negation flag
type operatorMapping struct {
	symbol string
	op     string
	negate bool
}

// operatorMappings defines the order of operator detection (longer operators first)
var operatorMappings = []operatorMapping{
	{"!~=", operator.Regex, true}, // not regex
	{"~=", operator.Regex, false}, // regex
	{"!=", operator.Equals, true}, // not equals
	{">=", operator.Gte, false},   // greater than or equal
	{"<=", operator.Lte, false},   // less than or equal
	{">", operator.Gt, false},     // greater than
	{"<", operator.Lt, false},     // less than
	{"=", operator.Equals, false}, // equals (must be last among = variants)
}

// IsHLSyntax detects if an expression uses hl syntax (has special operators)
func IsHLSyntax(expr string) bool {
	// Check for hl-style operators (longer ones first to avoid false positives)
	hlOperators := []string{"!~=", "~=", "!=", ">=", "<=", ">", "<"}
	for _, op := range hlOperators {
		if strings.Contains(expr, op) {
			return true
		}
	}
	return false
}

// ParseFilterFlag parses a single filter expression in hl syntax.
// Supports: key=value, key!=value, key~=value, key!~=value, key>value, key>=value, key<value, key<=value
func ParseFilterFlag(expr string) (*client.Filter, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("empty filter expression")
	}

	for _, mapping := range operatorMappings {
		idx := strings.Index(expr, mapping.symbol)
		if idx != -1 {
			field := strings.TrimSpace(expr[:idx])
			value := strings.TrimSpace(expr[idx+len(mapping.symbol):])

			if field == "" {
				return nil, fmt.Errorf("missing field name in filter expression: %s", expr)
			}

			// Remove surrounding quotes from value if present
			value = unquote(value)

			return &client.Filter{
				Field:  field,
				Op:     mapping.op,
				Value:  value,
				Negate: mapping.negate,
			}, nil
		}
	}

	return nil, fmt.Errorf("invalid filter expression (no operator found): %s", expr)
}

// ParseFilterFlags parses multiple -f flags into a combined AND filter.
// Each expression can be either legacy syntax (field=value) or hl syntax (field!=value, etc.)
func ParseFilterFlags(exprs []string) (*client.Filter, error) {
	if len(exprs) == 0 {
		return nil, nil
	}

	var filters []client.Filter
	for _, expr := range exprs {
		f, err := ParseFilterFlag(expr)
		if err != nil {
			return nil, err
		}
		filters = append(filters, *f)
	}

	if len(filters) == 1 {
		return &filters[0], nil
	}

	return &client.Filter{
		Logic:   client.LogicAnd,
		Filters: filters,
	}, nil
}

// ParseLegacyFilter parses a legacy filter expression (field=value with optional operator)
func ParseLegacyFilter(fieldExpr string, opExpr string) (*client.Filter, error) {
	fieldExpr = strings.TrimSpace(fieldExpr)
	if fieldExpr == "" {
		return nil, fmt.Errorf("empty field expression")
	}

	// Parse field=value
	idx := strings.Index(fieldExpr, "=")
	if idx == -1 {
		return nil, fmt.Errorf("invalid legacy filter expression (missing =): %s", fieldExpr)
	}

	field := strings.TrimSpace(fieldExpr[:idx])
	value := strings.TrimSpace(fieldExpr[idx+1:])

	if field == "" {
		return nil, fmt.Errorf("missing field name in legacy filter expression: %s", fieldExpr)
	}

	// Determine operator
	op := operator.Equals
	if opExpr != "" {
		switch opExpr {
		case operator.Match, operator.Wildcard, operator.Exists, operator.Regex:
			op = opExpr
		default:
			return nil, fmt.Errorf("invalid operator: %s", opExpr)
		}
	}

	return &client.Filter{
		Field: field,
		Op:    op,
		Value: value,
	}, nil
}

// unquote removes surrounding quotes from a string value
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
