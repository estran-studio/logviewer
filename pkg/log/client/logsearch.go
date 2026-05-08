package client

import (
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// VariableDefinition describes a dynamic parameter for a search context.
// This provides metadata to UIs and LLMs about what inputs are expected.
type VariableDefinition struct {
	Description string      `json:"description,omitempty"`
	Type        string      `json:"type,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Required    bool        `json:"required,omitempty"`
}

// SearchRange defines the time range for a search.
type SearchRange struct {
	Lte  ty.Opt[string] `json:"lte" yaml:"lte"`
	Gte  ty.Opt[string] `json:"gte" yaml:"gte"`
	Last ty.Opt[string] `json:"last" yaml:"last"`
}

// RefreshOptions defines options for auto-refreshing search results.
type RefreshOptions struct {
	Duration ty.Opt[string] `json:"duration,omitempty" yaml:"duration,omitempty"`
}

// FieldExtraction defines regex and keys for extracting fields from log messages.
type FieldExtraction struct {
	GroupRegex     ty.Opt[string] `json:"groupRegex,omitempty" yaml:"groupRegex,omitempty"`
	KvRegex        ty.Opt[string] `json:"kvRegex,omitempty" yaml:"kvRegex,omitempty"`
	TimestampRegex ty.Opt[string] `json:"timestampRegex,omitempty" yaml:"timestampRegex,omitempty"`

	JSON             ty.Opt[bool]   `json:"json,omitempty" yaml:"json,omitempty"`
	JSONMessageKey   ty.Opt[string] `json:"jsonMessageKey,omitempty" yaml:"jsonMessageKey,omitempty"`
	JSONLevelKey     ty.Opt[string] `json:"jsonLevelKey,omitempty" yaml:"jsonLevelKey,omitempty"`
	JSONTimestampKey ty.Opt[string] `json:"jsonTimestampKey,omitempty" yaml:"jsonTimestampKey,omitempty"`
}

// PrinterOptions defines options for printing log entries (template, color, etc.).
type PrinterOptions struct {
	Template     ty.Opt[string] `json:"template,omitempty" yaml:"template,omitempty"`
	MessageRegex ty.Opt[string] `json:"messageRegex,omitempty" yaml:"messageRegex,omitempty"`
	Color        ty.Opt[bool]   `json:"color,omitempty" yaml:"color,omitempty"`
}

// LogSearch defines the criteria for a log search operation.
type LogSearch struct {
	// NativeQuery allows passing a raw query string in the backend's native syntax
	// (e.g., Splunk SPL, OpenSearch DSL). Filters are appended to refine results.
	NativeQuery ty.Opt[string] `json:"nativeQuery,omitempty" yaml:"nativeQuery,omitempty"`

	// Current filterring fields (legacy - use Filter for complex queries)
	Fields ty.MS `json:"fields,omitempty" yaml:"fields,omitempty"`
	// Extra rules for filtering fields (legacy - use Filter for complex queries)
	FieldsCondition ty.MS `json:"fieldsCondition,omitempty" yaml:"fieldsCondition,omitempty"`

	// Filter is the new AST-based filter supporting nested logic (AND/OR/NOT)
	Filter *Filter `json:"filter,omitempty" yaml:"filter,omitempty"`

	// Range of the log query to do , depends of the system for full availability
	Range SearchRange `json:"range,omitempty" yaml:"range,omitempty"`

	// Max size of the request
	Size ty.Opt[int] `json:"size,omitempty" yaml:"size,omitempty"`

	// Refresh options for live data
	Refresh RefreshOptions `json:"refresh,omitempty" yaml:"refresh,omitempty"`

	// Options to configure the implementation with specific configuration for the search
	Options ty.MI `json:"options,omitempty" yaml:"options,omitempty"`

	// Token for fetching the next page of results
	PageToken ty.Opt[string] `json:"pageToken,omitempty" yaml:"pageToken,omitempty"`

	// Extra fields for field extraction for system without fieldging of log entry
	FieldExtraction FieldExtraction `json:"fieldExtraction,omitempty" yaml:"fieldExtraction,omitempty"`

	PrinterOptions PrinterOptions `json:"printerOptions,omitempty" yaml:"printerOptions,omitempty"`

	// Variables defines the dynamic inputs for this search context.
	// The map key is the variable name (e.g., "sessionId").
	Variables map[string]VariableDefinition `json:"variables,omitempty"`

	// Follow indicates if the search should continuously follow logs.
	Follow bool `json:"follow,omitempty" yaml:"follow,omitempty"`
}

// Clone creates a deep copy of the LogSearch object.
// This is useful when the same search configuration needs to be used in concurrent operations
// with slight modifications, preventing race conditions from shared state.
func (s *LogSearch) Clone() *LogSearch {
	if s == nil {
		return nil
	}

	clone := *s

	// Deep copy map fields
	if s.Options != nil {
		clone.Options = ty.MergeM(make(ty.MI), s.Options)
	}
	if s.Fields != nil {
		clone.Fields = ty.MergeM(make(ty.MS), s.Fields)
	}
	if s.FieldsCondition != nil {
		clone.FieldsCondition = ty.MergeM(make(ty.MS), s.FieldsCondition)
	}
	if s.Variables != nil {
		clone.Variables = make(map[string]VariableDefinition, len(s.Variables))
		for k, v := range s.Variables {
			clone.Variables[k] = v
		}
	}

	// Deep copy Filter if it exists
	if s.Filter != nil {
		clone.Filter = s.Filter.Clone()
	}

	return &clone
}

// GetEffectiveFilter returns a unified filter tree that combines legacy Fields/FieldsCondition
// with the new Filter field. This allows backward compatibility while supporting new AST filters.
func (s *LogSearch) GetEffectiveFilter() *Filter {
	var allFilters []Filter

	// 1. Convert Legacy Fields to Filter Nodes
	for field, value := range s.Fields {
		op := operator.Equals
		if condition, ok := s.FieldsCondition[field]; ok && condition != "" {
			op = condition
		}

		allFilters = append(allFilters, Filter{
			Field: field,
			Op:    op,
			Value: value,
		})
	}

	// 2. Add the Explicit New Filter (if it exists)
	if s.Filter != nil {
		allFilters = append(allFilters, *s.Filter)
	}

	if len(allFilters) == 0 {
		return nil
	}

	// If there is only one condition, return it directly
	if len(allFilters) == 1 {
		return &allFilters[0]
	}

	// Otherwise, wrap everything in an implicit root "AND"
	return &Filter{
		Logic:   LogicAnd,
		Filters: allFilters,
	}
}

// MergeInto merges another LogSearch into this one.
func (s *LogSearch) MergeInto(logSeach *LogSearch) error {

	if s.Fields == nil {
		s.Fields = ty.MS{}
	}
	if s.FieldsCondition == nil {
		s.FieldsCondition = ty.MS{}
	}
	if s.Options == nil {
		s.Options = ty.MI{}
	}
	if s.Variables == nil {
		s.Variables = make(map[string]VariableDefinition)
	}

	for k, v := range logSeach.Variables {
		s.Variables[k] = v
	}

	s.Fields = ty.MergeM(s.Fields, logSeach.Fields)
	s.FieldsCondition = ty.MergeM(s.FieldsCondition, logSeach.FieldsCondition)
	s.Options = ty.MergeM(s.Options, logSeach.Options)

	// Merge Filter: AND them together if both exist
	if logSeach.Filter != nil {
		if s.Filter != nil {
			s.Filter = &Filter{
				Logic:   LogicAnd,
				Filters: []Filter{*s.Filter, *logSeach.Filter},
			}
		} else {
			s.Filter = logSeach.Filter
		}
	}

	s.Size.Merge(&logSeach.Size)
	s.Refresh.Duration.Merge(&logSeach.Refresh.Duration)
	s.FieldExtraction.GroupRegex.Merge(&logSeach.FieldExtraction.GroupRegex)
	s.FieldExtraction.KvRegex.Merge(&logSeach.FieldExtraction.KvRegex)
	s.FieldExtraction.TimestampRegex.Merge(&logSeach.FieldExtraction.TimestampRegex)
	s.FieldExtraction.JSON.Merge(&logSeach.FieldExtraction.JSON)
	s.FieldExtraction.JSONMessageKey.Merge(&logSeach.FieldExtraction.JSONMessageKey)
	s.FieldExtraction.JSONLevelKey.Merge(&logSeach.FieldExtraction.JSONLevelKey)
	s.FieldExtraction.JSONTimestampKey.Merge(&logSeach.FieldExtraction.JSONTimestampKey)
	s.PrinterOptions.Template.Merge(&logSeach.PrinterOptions.Template)
	s.PrinterOptions.MessageRegex.Merge(&logSeach.PrinterOptions.MessageRegex)
	s.PrinterOptions.Color.Merge(&logSeach.PrinterOptions.Color)
	s.Range.Gte.Merge(&logSeach.Range.Gte)

	s.Range.Lte.Merge(&logSeach.Range.Lte)
	s.Range.Last.Merge(&logSeach.Range.Last)
	s.PageToken.Merge(&logSeach.PageToken)
	s.NativeQuery.Merge(&logSeach.NativeQuery)

	if logSeach.Follow {
		s.Follow = true
	}

	return nil
}
