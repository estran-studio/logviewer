package hl

import (
	"fmt"
	"strings"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
)

// BuildArgs constructs command-line arguments for the hl binary based on a LogSearch.
// It translates filters, time ranges, and other options into hl's CLI syntax.
//
// Parameters:
//   - search: The LogSearch containing filters, time range, and options
//   - paths: File paths to read logs from
//
// Returns the argument list (excluding the "hl" command itself) and any error.
func BuildArgs(search *client.LogSearch, paths []string) ([]string, error) {
	var args []string

	// Always disable pager for programmatic use
	args = append(args, "-P")

	// Output raw JSON entries (not human-readable formatted output)
	// This is essential for logviewer's reader to parse the JSON fields
	args = append(args, "--raw")

	// Handle follow mode
	if search.Follow {
		args = append(args, "-F")
	}

	// Handle time range
	timeArgs, err := buildTimeRangeArgs(search)
	if err != nil {
		return nil, fmt.Errorf("building time range: %w", err)
	}
	args = append(args, timeArgs...)

	// Handle size/limit

	// Build filter expression from the effective filter
	effectiveFilter := search.GetEffectiveFilter()
	if effectiveFilter != nil {
		filterExpr, err := buildFilterExpression(effectiveFilter)
		if err != nil {
			return nil, fmt.Errorf("building filter expression: %w", err)
		}
		if filterExpr != "" {
			args = append(args, "-q", filterExpr)
		}
	}

	// Add file paths at the end
	args = append(args, paths...)

	return args, nil
}

// buildTimeRangeArgs constructs --since and --until arguments from SearchRange.
func buildTimeRangeArgs(search *client.LogSearch) ([]string, error) {
	var args []string

	// Handle "Last" (relative duration like "15m", "1h")
	if search.Range.Last.Set && search.Range.Last.Value != "" {
		// hl accepts formats like "-15m", "-1h", "-3d"
		// Our format is "15m", "1h" - need to prefix with "-"
		duration := search.Range.Last.Value
		if !strings.HasPrefix(duration, "-") {
			duration = "-" + duration
		}
		args = append(args, "--since", duration)
	}

	// Handle explicit Gte (start time)
	if search.Range.Gte.Set && search.Range.Gte.Value != "" {
		args = append(args, "--since", search.Range.Gte.Value)
	}

	// Handle explicit Lte (end time)
	if search.Range.Lte.Set && search.Range.Lte.Value != "" {
		args = append(args, "--until", search.Range.Lte.Value)
	}

	return args, nil
}

// buildFilterExpression converts a Filter AST to an hl query expression string.
func buildFilterExpression(filter *client.Filter) (string, error) {
	if filter == nil {
		return "", nil
	}

	// Handle branch (AND/OR/NOT)
	if filter.Logic != "" {
		return buildBranchExpression(filter)
	}

	// Handle leaf (condition)
	if filter.Field != "" {
		return buildLeafExpression(filter)
	}

	// Empty filter
	return "", nil
}

// buildBranchExpression handles AND/OR/NOT logic operators.
func buildBranchExpression(filter *client.Filter) (string, error) {
	if len(filter.Filters) == 0 {
		return "", nil
	}

	var parts []string
	for _, child := range filter.Filters {
		expr, err := buildFilterExpression(&child)
		if err != nil {
			return "", err
		}
		if expr != "" {
			parts = append(parts, expr)
		}
	}

	if len(parts) == 0 {
		return "", nil
	}

	switch filter.Logic {
	case client.LogicAnd:
		// Wrap each part in parentheses for safety
		if len(parts) == 1 {
			return parts[0], nil
		}
		return "(" + strings.Join(parts, " and ") + ")", nil

	case client.LogicOr:
		if len(parts) == 1 {
			return parts[0], nil
		}
		return "(" + strings.Join(parts, " or ") + ")", nil

	case client.LogicNot:
		// NOT applies to all children ANDed together
		if len(parts) == 1 {
			return "not (" + parts[0] + ")", nil
		}
		return "not (" + strings.Join(parts, " and ") + ")", nil

	default:
		return "", fmt.Errorf("unknown logic operator: %s", filter.Logic)
	}
}

// buildLeafExpression converts a single filter condition to hl syntax.
func buildLeafExpression(filter *client.Filter) (string, error) {
	field := filter.Field
	value := filter.Value
	op := filter.Op
	negate := filter.Negate

	// Handle special "_" field (message search)
	if field == "_" {
		// For message search, use the message field or raw text matching
		field = "message"
	}

	// Escape value for hl query syntax
	escapedValue := escapeValue(value)

	// Map operator to hl syntax
	hlOp, err := mapOperator(op, negate)
	if err != nil {
		return "", err
	}

	// Handle exists operator specially
	if op == operator.Exists {
		if negate {
			return fmt.Sprintf("not exists(.%s)", field), nil
		}
		return fmt.Sprintf("exists(.%s)", field), nil
	}

	// Build the expression
	// hl uses .fieldname for field references, but simple names work too
	return fmt.Sprintf("%s %s %s", field, hlOp, escapedValue), nil
}

// mapOperator converts logviewer operator to hl operator syntax.
func mapOperator(op string, negate bool) (string, error) {
	switch op {
	case "", operator.Equals:
		if negate {
			return "!=", nil
		}
		return "=", nil

	case operator.Regex:
		if negate {
			return "!~~=", nil
		}
		return "~~=", nil

	case operator.Match:
		// Match is case-insensitive contains in logviewer
		// hl's ~= is contain (substring)
		if negate {
			return "!~=", nil
		}
		return "~=", nil

	case operator.Wildcard:
		if negate {
			return "not like", nil
		}
		return "like", nil

	case operator.Gt:
		return ">", nil

	case operator.Gte:
		return ">=", nil

	case operator.Lt:
		return "<", nil

	case operator.Lte:
		return "<=", nil

	case operator.Exists:
		// Handled specially in buildLeafExpression
		return "", nil

	default:
		return "", fmt.Errorf("unsupported operator: %s", op)
	}
}

// escapeValue escapes a value for use in hl query expressions.
// Values containing special characters are wrapped in quotes.
func escapeValue(value string) string {
	// Check if value needs quoting
	needsQuoting := false
	for _, c := range value {
		if c == ' ' || c == '(' || c == ')' || c == '\'' || c == '"' ||
			c == '>' || c == '<' || c == '=' || c == '!' || c == '~' ||
			c == '&' || c == '|' {
			needsQuoting = true
			break
		}
	}

	if !needsQuoting {
		return value
	}

	// Escape double quotes and wrap in double quotes
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// BuildSimpleArgs constructs arguments for simple filtering scenarios
// where we only need basic field=value filters without complex logic.
func BuildSimpleArgs(paths []string, follow bool, since string, filters map[string]string) []string {
	var args []string

	args = append(args, "-P")    // Disable pager
	args = append(args, "--raw") // Output raw JSON

	if follow {
		args = append(args, "-F")
	}

	if since != "" {
		if !strings.HasPrefix(since, "-") {
			since = "-" + since
		}
		args = append(args, "--since", since)
	}

	// Add simple filters using -f flag
	for field, value := range filters {
		args = append(args, "-f", fmt.Sprintf("%s=%s", field, value))
	}

	args = append(args, paths...)

	return args
}
