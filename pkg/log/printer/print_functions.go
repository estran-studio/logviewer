package printer

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/TylerBrock/colorjson"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/fatih/color"
)

// FindJSON finds all valid JSON objects and arrays in a string.
// Returns slices of JSON strings found in the input.
func FindJSON(s string) []string {
	var results []string
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		if runes[i] != '{' && runes[i] != '[' {
			continue
		}

		// Try to extract JSON starting at this position
		if jsonStr := ExtractJSON(runes, i); jsonStr != "" {
			// Validate it's actually valid JSON
			var testObj interface{}
			if err := json.Unmarshal([]byte(jsonStr), &testObj); err == nil {
				// Check it's not empty
				switch v := testObj.(type) {
				case map[string]interface{}:
					if len(v) > 0 {
						results = append(results, jsonStr)
					}
				case []interface{}:
					if len(v) > 0 {
						results = append(results, jsonStr)
					}
				default:
					results = append(results, jsonStr)
				}
			}
			// Skip past this JSON to avoid finding nested objects
			i += len([]rune(jsonStr)) - 1
		}
	}

	return results
}

// ExtractJSON attempts to extract a complete JSON object or array starting at position start.
// Returns the JSON string if valid, or empty string if not.
func ExtractJSON(runes []rune, start int) string {
	if start >= len(runes) {
		return ""
	}

	openChar := runes[start]
	if openChar != '{' && openChar != '[' {
		return ""
	}

	depth := 0
	inString := false
	escape := false

	for i := start; i < len(runes); i++ {
		r := runes[i]

		if escape {
			escape = false
			continue
		}

		if r == '\\' {
			escape = true
			continue
		}

		if r == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		switch r {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return string(runes[start : i+1])
			}
		}
	}

	return ""
}

// FormatDate formats a time.Time object according to the layout.
func FormatDate(layout string, t time.Time) string {
	return t.Format(layout)
}

// FormatTimestamp formats a timestamp in local time, returning "N/A" for zero-value timestamps.
// This is useful for aggregated results (stats, timechart) where timestamps may be unknown.
// Converting to local time ensures the displayed time matches what users can type in --from/--to.
// Usage in template: {{FormatTimestamp .Timestamp "15:04:05"}}
func FormatTimestamp(t time.Time, layout string) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Local().Format(layout)
}

// MultilineFields formats map fields into a multiline string prefixed with " * ".
func MultilineFields(values ty.MI) string {
	str := ""

	for k, v := range values {
		switch value := v.(type) {
		case string:
			str += fmt.Sprintf("\n * %s=%s", k, value)
		default:
			continue
		}
	}

	return str
}

// KV formats map fields into a key=value string.
func KV(values ty.MI) string {
	items := make([]string, 0, len(values))
	for k, v := range values {
		items = append(items, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(items, " ")
}

// ExpandJSON detects and formats all JSON objects and arrays in the message.
// Outputs formatted, indented (and colored if enabled) JSON on new lines.
// Usage in template: {{.Message}}{{ExpandJson .Message}}
func ExpandJSON(value string) string {
	jsonStrings := FindJSON(value)
	if len(jsonStrings) == 0 {
		return ""
	}

	var result strings.Builder
	for _, jsonStr := range jsonStrings {
		var obj interface{}
		if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
			continue
		}

		// Format with indentation and color (if enabled)
		if IsColorEnabled() {
			f := colorjson.NewFormatter()
			f.Indent = 2
			formatted, err := f.Marshal(obj)
			if err != nil {
				continue
			}
			result.WriteString("\n")
			result.Write(formatted)
		} else {
			// Plain formatting without colors
			formatted, err := json.MarshalIndent(obj, "", "  ")
			if err != nil {
				continue
			}
			result.WriteString("\n")
			result.Write(formatted)
		}
	}

	return result.String()
}

// ExpandJSONLimit detects and formats JSON with a maximum line limit.
// If the formatted JSON exceeds maxLines, it's truncated with "... (truncated)" indicator.
// Usage in template: {{ExpandJsonLimit .Message 10}}
func ExpandJSONLimit(value string, maxLines int) string {
	fullExpanded := ExpandJSON(value)
	if fullExpanded == "" {
		return ""
	}

	lines := strings.Split(fullExpanded, "\n")
	if len(lines) <= maxLines+1 { // +1 because first line is empty
		return fullExpanded
	}

	// Truncate and add indicator
	truncated := strings.Join(lines[:maxLines+1], "\n")
	return truncated + "\n  ... (truncated, " + fmt.Sprintf("%d", len(lines)-maxLines-1) + " more lines)"
}

// ExpandJSONLimitDepth detects and formats JSON with a maximum depth limit.
// Nested objects/arrays beyond maxDepth are replaced with "..." indicator.
// Useful for preventing deeply nested JSON from cluttering output.
// Usage in template: {{ExpandJsonLimitDepth .Message 3}}
func ExpandJSONLimitDepth(value string, maxDepth int) string {
	jsonStrings := FindJSON(value)
	if len(jsonStrings) == 0 {
		return ""
	}

	var result strings.Builder
	for _, jsonStr := range jsonStrings {
		var obj interface{}
		if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
			continue
		}

		// Truncate to max depth
		truncated := truncateDepth(obj, maxDepth, 0)

		// Format with indentation and color (if enabled)
		if IsColorEnabled() {
			f := colorjson.NewFormatter()
			f.Indent = 2
			formatted, err := f.Marshal(truncated)
			if err != nil {
				continue
			}
			result.WriteString("\n")
			result.Write(formatted)
		} else {
			// Plain formatting without colors
			formatted, err := json.MarshalIndent(truncated, "", "  ")
			if err != nil {
				continue
			}
			result.WriteString("\n")
			result.Write(formatted)
		}
	}

	return result.String()
}

// truncateDepth recursively truncates JSON structures beyond maxDepth.
// Returns a new structure with deep nesting replaced by "..." strings.
func truncateDepth(obj interface{}, maxDepth, currentDepth int) interface{} {
	if currentDepth >= maxDepth {
		return "..."
	}

	switch v := obj.(type) {
	case map[string]interface{}:
		if len(v) == 0 {
			return v
		}
		truncated := make(map[string]interface{}, len(v))
		for key, val := range v {
			truncated[key] = truncateDepth(val, maxDepth, currentDepth+1)
		}
		return truncated

	case []interface{}:
		if len(v) == 0 {
			return v
		}
		truncated := make([]interface{}, len(v))
		for i, val := range v {
			truncated[i] = truncateDepth(val, maxDepth, currentDepth+1)
		}
		return truncated

	default:
		// Primitive types (string, number, bool, null) - return as-is
		return v
	}
}

// ExpandJSONCompact detects and formats JSON on a single line (no indentation).
// Useful for short JSON payloads where vertical space is limited.
// Usage in template: {{ExpandJsonCompact .Message}}
func ExpandJSONCompact(value string) string {
	jsonStrings := FindJSON(value)
	if len(jsonStrings) == 0 {
		return ""
	}

	var result strings.Builder
	for _, jsonStr := range jsonStrings {
		var obj interface{}
		if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
			continue
		}

		// Format on single line
		formatted, err := json.Marshal(obj)
		if err != nil {
			continue
		}
		result.WriteString("\n")
		result.Write(formatted)
	}

	return result.String()
}

// GetField provides case-insensitive field access for templates.
// Usage in template: {{Field . "level"}} or {{Field . "thread"}}
func GetField(fields ty.MI, key string) interface{} {
	// Try exact match first
	if val, ok := fields[key]; ok {
		return val
	}
	// Try capitalized version (common for struct fields)
	if len(key) > 0 {
		capKey := string(key[0]-32) + key[1:]
		if val, ok := fields[capKey]; ok {
			return val
		}
	}
	return ""
}

// Trim removes leading and trailing whitespace from a string.
// Usage in template: {{Trim .Message}} or {{.Message | Trim}}
func Trim(s string) string {
	return strings.TrimSpace(s)
}

// ColorLevel applies color based on log level.
// Usage in template: {{ColorLevel .Level}}
// Color mapping: ERROR/FATAL/CRITICAL=red, WARN/WARNING=yellow, INFO=cyan, DEBUG=blue, TRACE=dim
func ColorLevel(level string) string {
	if !IsColorEnabled() {
		return level
	}

	levelUpper := strings.ToUpper(strings.TrimSpace(level))

	switch levelUpper {
	case "ERROR", "FATAL", "CRITICAL":
		return color.RedString(level)
	case "WARN", "WARNING":
		return color.YellowString(level)
	case "INFO":
		return color.CyanString(level)
	case "DEBUG":
		return color.BlueString(level)
	case "TRACE":
		return color.New(color.FgHiBlack).Sprint(level)
	default:
		return level
	}
}

// ColorTimestamp colors timestamp in dim gray.
// Usage in template: {{ColorTimestamp (FormatTimestamp .Timestamp "15:04:05")}}
func ColorTimestamp(timestamp string) string {
	if !IsColorEnabled() {
		return timestamp
	}
	return color.New(color.FgHiBlack).Sprint(timestamp)
}

// ColorContext colors context ID in magenta.
// Usage in template: {{ColorContext .ContextID}}
func ColorContext(contextID string) string {
	if !IsColorEnabled() {
		return contextID
	}
	return color.MagentaString(contextID)
}

// ColorString applies a named color to text.
// Usage in template: {{ColorString "red" "ERROR"}} or {{ColorString "green" .Message}}
// Available colors: red, green, yellow, blue, magenta, cyan, white, black, dim/gray/grey
func ColorString(colorName, text string) string {
	if !IsColorEnabled() {
		return text
	}

	switch strings.ToLower(colorName) {
	case "red":
		return color.RedString(text)
	case "green":
		return color.GreenString(text)
	case "yellow":
		return color.YellowString(text)
	case "blue":
		return color.BlueString(text)
	case "magenta":
		return color.MagentaString(text)
	case "cyan":
		return color.CyanString(text)
	case "white":
		return color.WhiteString(text)
	case "black":
		return color.BlackString(text)
	case "dim", "gray", "grey":
		return color.New(color.FgHiBlack).Sprint(text)
	default:
		return text
	}
}

// Bold makes text bold.
// Usage in template: {{Bold "Important Message"}} or {{Bold .Level}}
func Bold(text string) string {
	if !IsColorEnabled() {
		return text
	}
	return color.New(color.Bold).Sprint(text)
}

// GetTemplateFunctionsMap returns a map of custom template functions.
func GetTemplateFunctionsMap() template.FuncMap {
	return template.FuncMap{
		"Format":               FormatDate,
		"FormatTimestamp":      FormatTimestamp,
		"MultiLine":            MultilineFields,
		"ExpandJson":           ExpandJSON,
		"ExpandJsonLimit":      ExpandJSONLimit,
		"ExpandJsonLimitDepth": ExpandJSONLimitDepth,
		"ExpandJsonCompact":    ExpandJSONCompact,
		"Field":                GetField,
		"KV":                   KV,
		"Trim":                 Trim,
		// Color functions
		"ColorLevel":     ColorLevel,
		"ColorTimestamp": ColorTimestamp,
		"ColorContext":   ColorContext,
		"ColorString":    ColorString,
		"Bold":           Bold,
	}
}
