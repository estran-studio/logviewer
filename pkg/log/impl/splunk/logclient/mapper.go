package logclient

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// transformingCommands lists Splunk commands that transform events into results.
// These commands require fetching from /results endpoint instead of /events.
var transformingCommands = []string{
	"stats", "chart", "timechart", "top", "rare",
	"transaction", "cluster", "kmeans",
	"eventstats", "streamstats",
	"bucket", "bin",
	"predict", "trendline",
	"geostats", "sichart", "sitimechart",
	"mstats", "tstats",
	"table",
}

// transformingCommandPattern matches pipe followed by a transforming command.
// Uses word boundary to avoid matching partial words (e.g., "topaz" shouldn't match "top").
var transformingCommandPattern *regexp.Regexp

// fieldsCommandPattern matches | fields without the + modifier
// "| fields +" is NOT transforming (just selects fields from events)
// "| fields" or "| fields -" IS transforming (changes event structure)
var fieldsCommandPattern *regexp.Regexp

func init() {
	// Build pattern: | followed by optional whitespace, then one of the commands as a word
	pattern := `\|\s*(` + strings.Join(transformingCommands, "|") + `)(?:\s|$)`
	transformingCommandPattern = regexp.MustCompile("(?i)" + pattern)

	// Match "| fields" not followed by "+"
	// This catches "| fields" or "| fields -" but not "| fields +"
	fieldsCommandPattern = regexp.MustCompile(`(?i)\|\s*fields(?:\s+[^+\s]|\s*$)`)
}

// ContainsTransformingCommand checks if a Splunk query contains transforming commands
// that require results to be fetched from the /results endpoint instead of /events.
func ContainsTransformingCommand(query string) bool {
	// Check standard transforming commands
	if transformingCommandPattern.MatchString(query) {
		return true
	}

	// Check for "| fields" without "+" modifier (which IS transforming)
	// "| fields +" is NOT transforming, so we exclude it
	return fieldsCommandPattern.MatchString(query)
}

func escapeSplunkValue(value string) string {
	return strings.ReplaceAll(value, "\"", "\\\"")
}

// buildSplunkCondition builds a single condition for Splunk search.
// Returns the condition string and a boolean indicating if it's a regex (needs pipe).
func buildSplunkCondition(f *client.Filter) (condition string, isRegex bool) {
	if f.Field == "" {
		return "", false
	}

	op := f.Op
	if op == "" {
		op = operator.Equals
	}

	var cond string
	var isRegexCond bool

	// Handle free-text search (field is "_" or empty)
	if f.Field == "_" {
		switch {
		case op == operator.Regex:
			cond = fmt.Sprintf(`regex _raw="%s"`, escapeSplunkValue(f.Value))
			isRegexCond = true
		case strings.Contains(f.Value, " "):
			cond = fmt.Sprintf(`"%s"`, escapeSplunkValue(f.Value))
		default:
			cond = escapeSplunkValue(f.Value)
		}
	} else {
		switch op {
		case operator.Regex:
			cond = fmt.Sprintf(`regex %s="%s"`, f.Field, escapeSplunkValue(f.Value))
			isRegexCond = true
		case operator.Wildcard:
			cond = fmt.Sprintf(`%s="%s*"`, f.Field, escapeSplunkValue(f.Value))
		case operator.Exists:
			cond = fmt.Sprintf(`%s=*`, f.Field)
		case operator.Gt:
			cond = fmt.Sprintf(`%s>%s`, f.Field, f.Value)
		case operator.Gte:
			cond = fmt.Sprintf(`%s>=%s`, f.Field, f.Value)
		case operator.Lt:
			cond = fmt.Sprintf(`%s<%s`, f.Field, f.Value)
		case operator.Lte:
			cond = fmt.Sprintf(`%s<=%s`, f.Field, f.Value)
		default: // equals, match
			cond = fmt.Sprintf(`%s="%s"`, f.Field, escapeSplunkValue(f.Value))
		}
	}

	// Handle negation
	if f.Negate {
		if isRegexCond {
			// The `regex` command doesn't support inline negation.
			// Use `where NOT match(...)` for negated regex, which is valid SPL.
			field := f.Field
			if field == "_" {
				field = "_raw"
			}
			cond = fmt.Sprintf(`where NOT match(%s, "%s")`, field, escapeSplunkValue(f.Value))
			isRegexCond = false // where command is not a regex pipe command
		} else {
			cond = fmt.Sprintf("NOT (%s)", cond)
		}
	}

	return cond, isRegexCond
}

// buildSplunkQuery recursively builds a Splunk search query from a Filter AST.
// It returns the main query string and a slice of regex conditions that need pipe commands.
func buildSplunkQuery(f *client.Filter) (query string, regexConditions []string) {
	if f == nil {
		return "", nil
	}

	// Handle Leaf (Condition)
	if f.Field != "" {
		cond, isRegex := buildSplunkCondition(f)
		if isRegex {
			return "", []string{cond}
		}
		return cond, nil
	}

	// Handle Branch (Group)
	if f.Logic == "" || len(f.Filters) == 0 {
		return "", nil
	}

	var parts []string
	var allRegex []string

	for _, child := range f.Filters {
		childQuery, childRegex := buildSplunkQuery(&child)
		if childQuery != "" {
			parts = append(parts, childQuery)
		}
		allRegex = append(allRegex, childRegex...)
	}

	if len(parts) == 0 {
		return "", allRegex
	}

	var result string
	switch f.Logic {
	case client.LogicAnd:
		// Splunk uses space for implicit AND
		result = strings.Join(parts, " ")
	case client.LogicOr:
		// Splunk uses OR keyword
		result = strings.Join(parts, " OR ")
	case client.LogicNot:
		// NOT applies to all children (ANDed together, then inverted)
		inner := strings.Join(parts, " ")
		result = fmt.Sprintf("NOT (%s)", inner)
	}

	// Wrap in parentheses if multiple parts (for proper precedence)
	if len(parts) > 1 && f.Logic != client.LogicNot {
		result = "(" + result + ")"
	}

	return result, allRegex
}

// trimTrailingPipe removes trailing whitespace and pipe characters from a query string.
// This prevents invalid queries like "index=main | | search ..." when appending filters.
func trimTrailingPipe(query string) string {
	query = strings.TrimRight(query, " \t\n\r")
	for strings.HasSuffix(query, "|") {
		query = strings.TrimSuffix(query, "|")
		query = strings.TrimRight(query, " \t\n\r")
	}
	return query
}

func getSearchRequest(logSearch *client.LogSearch) (ty.MS, error) {
	ms := ty.MS{
		"earliest_time": logSearch.Range.Gte.Value,
		"latest_time":   logSearch.Range.Lte.Value,
	}

	// If the caller provided a `last` duration (e.g. "1min"), prefer that
	// over explicit gte/lte. We translate it to an earliest_time of
	// "-<last>" and latest_time of "now" which Splunk understands as a
	// relative time window.
	if logSearch.Range.Last.Value != "" {
		ms["earliest_time"] = "-" + logSearch.Range.Last.Value
		ms["latest_time"] = "now"
	}

	var query strings.Builder
	hasNativeQuery := logSearch.NativeQuery.Set && logSearch.NativeQuery.Value != ""

	// 1. Start with Native Query if provided (trimmed of trailing pipes)
	if hasNativeQuery {
		query.WriteString(trimTrailingPipe(logSearch.NativeQuery.Value))
	}

	// 2. Add index if specified - but ONLY if no native query is provided.
	// When using nativeQuery, the user has full control over the index in their query.
	if !hasNativeQuery {
		if index, ok := logSearch.Options.GetStringOk("index"); ok {
			query.WriteString(fmt.Sprintf("index=%s", index))
		}
	}

	// 3. Get the effective filter (combines legacy Fields with new Filter)
	effectiveFilter := logSearch.GetEffectiveFilter()

	// Collect all additional search conditions to append as a single | search command
	var searchConditions []string
	var regexConditions []string

	if effectiveFilter != nil {
		filterQuery, regexConds := buildSplunkQuery(effectiveFilter)
		if filterQuery != "" {
			searchConditions = append(searchConditions, filterQuery)
		}
		regexConditions = regexConds
	}

	// Append all search conditions as a single | search command (if we have a native query)
	// or space-joined (if building from scratch)
	if len(searchConditions) > 0 {
		combinedConditions := strings.Join(searchConditions, " ")
		if query.Len() > 0 {
			if hasNativeQuery {
				// Use single pipe search for all conditions
				query.WriteString(" | search ")
			} else {
				query.WriteString(" ")
			}
		}
		query.WriteString(combinedConditions)
	}

	// Add regex conditions as separate pipe commands (they cannot be combined)
	for _, regex := range regexConditions {
		if query.Len() > 0 {
			query.WriteString(" | ")
		}
		query.WriteString(regex)
	}

	// Add fields selection if specified
	if fields, ok := logSearch.Options.GetListOfStringsOk("fields"); ok {
		if len(fields) > 0 {
			query.WriteString(fmt.Sprintf(" | fields + %s", strings.Join(fields, ", ")))
		}
	}

	ms["search"] = query.String()

	return ms, nil
}
