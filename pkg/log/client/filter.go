package client

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/estran-studio/logviewer/pkg/log/client/operator"
)

// LogicOperator defines logical operators for combining filters (AND, OR, NOT).
type LogicOperator string

const (
	// LogicAnd combines filters with AND logic.
	LogicAnd LogicOperator = "AND"
	// LogicOr combines filters with OR logic.
	LogicOr LogicOperator = "OR"
	// LogicNot inverts the result of the filter.
	LogicNot LogicOperator = "NOT"
)

// Filter represents a recursive filter AST node.
// It can be either a leaf node (condition) or a branch node (group).
type Filter struct {
	// --- Leaf Node (Condition) ---
	// If Field is set, this is a condition
	Field  string `json:"field,omitempty" yaml:"field,omitempty"`
	Op     string `json:"op,omitempty" yaml:"op,omitempty"` // e.g., "equals", "regex", "wildcard", "exists", "match", "gt", "gte", "lt", "lte"
	Value  string `json:"value,omitempty" yaml:"value,omitempty"`
	Negate bool   `json:"negate,omitempty" yaml:"negate,omitempty"` // For != and !~= operators

	// --- Branch Node (Group) ---
	// If Logic is set, this is a group
	Logic   LogicOperator `json:"logic,omitempty" yaml:"logic,omitempty"`
	Filters []Filter      `json:"filters,omitempty" yaml:"filters,omitempty"`
}

// Clone creates a deep copy of the Filter object.
// This is essential for preventing race conditions when the same filter
// is used in concurrent operations with modifications.
func (f *Filter) Clone() *Filter {
	if f == nil {
		return nil
	}

	// Start with a shallow copy
	clone := *f

	// Deep copy nested Filters slice
	if len(f.Filters) > 0 {
		clone.Filters = make([]Filter, len(f.Filters))
		for i, child := range f.Filters {
			if childClone := child.Clone(); childClone != nil {
				clone.Filters[i] = *childClone
			}
		}
	}

	return &clone
}

// Validate checks if the filter is structurally valid.
func (f *Filter) Validate() error {
	if f == nil {
		return nil
	}

	isLeaf := f.Field != ""
	isBranch := f.Logic != ""

	// A filter must be either a leaf or a branch, not both
	if isLeaf && isBranch {
		return fmt.Errorf("filter cannot have both 'field' and 'logic' set")
	}

	// Empty filter (neither leaf nor branch) is valid and means "match all"
	if !isLeaf && !isBranch {
		return nil
	}

	// Validate leaf node
	if isLeaf {
		// Validate operator
		switch f.Op {
		case "", operator.Equals, operator.Match, operator.Wildcard, operator.Exists, operator.Regex,
			operator.Gt, operator.Gte, operator.Lt, operator.Lte:
			// valid
		default:
			return fmt.Errorf("invalid operator: %s", f.Op)
		}

		// 'exists' operator doesn't need a value, others do
		if f.Op != operator.Exists && f.Value == "" {
			return fmt.Errorf("filter with field '%s' requires a value (unless op is 'exists')", f.Field)
		}

		// Leaf nodes shouldn't have children
		if len(f.Filters) > 0 {
			return fmt.Errorf("leaf filter (field='%s') cannot have nested filters", f.Field)
		}
	}

	// Validate branch node
	if isBranch {
		// Validate logic operator
		switch f.Logic {
		case LogicAnd, LogicOr, LogicNot:
			// valid
		default:
			return fmt.Errorf("invalid logic operator: %s", f.Logic)
		}

		// NOT should ideally have at least one child
		if f.Logic == LogicNot && len(f.Filters) == 0 {
			return fmt.Errorf("NOT filter must have at least one child filter")
		}

		// Branch nodes shouldn't have leaf properties
		if f.Value != "" {
			return fmt.Errorf("branch filter (logic='%s') should not have a value", f.Logic)
		}

		// Recursively validate children
		for i, child := range f.Filters {
			if err := child.Validate(); err != nil {
				return fmt.Errorf("filter[%d]: %w", i, err)
			}
		}
	}

	return nil
}

// Match evaluates the filter against a LogEntry (client-side filtering).
func (f *Filter) Match(entry LogEntry) bool {
	if f == nil {
		return true
	}

	// Handle Branch (Group)
	if f.Logic != "" {
		return f.matchBranch(entry)
	}

	// Handle Leaf (Condition)
	if f.Field != "" {
		return f.matchLeaf(entry)
	}

	// Empty filter matches everything
	return true
}

func (f *Filter) matchBranch(entry LogEntry) bool {
	if len(f.Filters) == 0 {
		return true // Empty group matches everything
	}

	switch f.Logic {
	case LogicAnd:
		for _, child := range f.Filters {
			if !child.Match(entry) {
				return false
			}
		}
		return true

	case LogicOr:
		for _, child := range f.Filters {
			if child.Match(entry) {
				return true
			}
		}
		return false

	case LogicNot:
		// NOT inverts the result of all children ANDed together
		for _, child := range f.Filters {
			if !child.Match(entry) {
				return true // If any child doesn't match, NOT matches
			}
		}
		return false
	}

	return true
}

func (f *Filter) matchLeaf(entry LogEntry) bool {
	// Handle special "_" sentinel for raw message search
	if f.Field == "_" {
		return f.matchValue(entry.Message)
	}

	// Use LogEntry.Field() for consistent field access (handles case-insensitivity and struct fields)
	fieldValRaw := entry.Field(f.Field)

	// Handle "exists" operator
	if f.Op == operator.Exists {
		return fieldValRaw != "" && fieldValRaw != nil
	}

	// Convert to string for comparison
	fieldVal := toString(fieldValRaw)

	// If field is missing/empty, no match (except for exists which is handled above)
	if fieldVal == "" {
		return false
	}

	return f.matchValue(fieldVal)
}

func (f *Filter) matchValue(fieldVal string) bool {
	var result bool

	switch f.Op {
	case operator.Regex:
		matched, err := regexp.MatchString(f.Value, fieldVal)
		if err != nil {
			result = false
		} else {
			result = matched
		}

	case operator.Wildcard:
		// Convert glob pattern to regex: * -> .*, ? -> .
		pattern := regexp.QuoteMeta(f.Value)
		pattern = strings.ReplaceAll(pattern, `\*`, `.*`)
		pattern = strings.ReplaceAll(pattern, `\?`, `.`)
		pattern = "^" + pattern + "$"
		matched, err := regexp.MatchString(pattern, fieldVal)
		if err != nil {
			result = false
		} else {
			result = matched
		}

	case operator.Match:
		// Match is a case-insensitive contains
		result = strings.Contains(strings.ToLower(fieldVal), strings.ToLower(f.Value))

	case operator.Gt, operator.Gte, operator.Lt, operator.Lte:
		result = f.compareNumeric(fieldVal)

	case "", operator.Equals:
		result = fieldVal == f.Value

	default:
		// Unknown operator, default to equals
		result = fieldVal == f.Value
	}

	// Apply negation if set
	if f.Negate {
		return !result
	}
	return result
}

// compareNumeric compares field value with filter value as numbers.
// Falls back to string comparison if parsing fails.
func (f *Filter) compareNumeric(fieldVal string) bool {
	fieldNum, err1 := strconv.ParseFloat(fieldVal, 64)
	valueNum, err2 := strconv.ParseFloat(f.Value, 64)

	// If either value can't be parsed as a number, fall back to string comparison
	if err1 != nil || err2 != nil {
		return f.compareString(fieldVal)
	}

	switch f.Op {
	case operator.Gt:
		return fieldNum > valueNum
	case operator.Gte:
		return fieldNum >= valueNum
	case operator.Lt:
		return fieldNum < valueNum
	case operator.Lte:
		return fieldNum <= valueNum
	}
	return false
}

// compareString compares field value with filter value as strings.
func (f *Filter) compareString(fieldVal string) bool {
	switch f.Op {
	case operator.Gt:
		return fieldVal > f.Value
	case operator.Gte:
		return fieldVal >= f.Value
	case operator.Lt:
		return fieldVal < f.Value
	case operator.Lte:
		return fieldVal <= f.Value
	}
	return false
}

// toString converts an interface{} to string for comparison
func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}
