// Package tui provides the terminal user interface components.
package tui

import "github.com/estran-studio/logviewer/pkg/log/client"

// ChipType categorizes different search components
type ChipType int

const (
	// ChipTypeField represents a field=value filter (e.g., level=ERROR)
	ChipTypeField ChipType = iota
	// ChipTypeVariable represents a variable reference (e.g., $sessionId)
	ChipTypeVariable
	// ChipTypeFreeText represents plain text search
	ChipTypeFreeText
	// ChipTypeTimeRange represents time range (e.g., last:1h, from:2024-01-01, to:now)
	ChipTypeTimeRange
	// ChipTypeVarAssign represents variable assignment (e.g., $userId=123)
	ChipTypeVarAssign
	// ChipTypeNativeQuery represents a native backend query (SPL, Lucene, etc.)
	ChipTypeNativeQuery
	// ChipTypeFilterGroup represents an OR/AND/NOT group of filters
	ChipTypeFilterGroup
	// ChipTypeSize represents result size limit (e.g., size:100)
	ChipTypeSize
	// ChipTypeContext represents the active context (informational only)
	ChipTypeContext
	// ChipTypeInherit represents an inherited search template (informational only)
	ChipTypeInherit
	// ChipTypeOption represents a backend-specific search option (e.g., index, sourcetype)
	ChipTypeOption
)

// Chip represents a single search component in the chip-based search bar
type Chip struct {
	Type     ChipType // Type of chip
	Field    string   // For ChipTypeField: the field name
	Operator string   // For ChipTypeField: =, !=, ~=, >, <, >=, <=
	Value    string   // For ChipTypeField/Variable: the value or var name
	Text     string   // For ChipTypeFreeText: the search text
	Display  string   // Rendered display string for the chip

	// For ChipTypeFilterGroup: complex filter groups
	GroupLogic  string         // "OR", "AND", "NOT"
	GroupFilter *client.Filter // Original filter for rebuilding search
	Editable    bool           // Whether this chip can be edited (false for complex groups)
}

// ChipSearchState manages the chip-based search input state
type ChipSearchState struct {
	// Committed chips
	Chips []Chip

	// Current input being typed
	CurrentInput   string
	CursorPosition int

	// Chip navigation (-1 if typing in input, 0+ if navigating chips)
	SelectedChip int

	// Autocomplete state
	AutocompleteOpen        bool
	AutocompleteSuggestions []Suggestion
	AutocompleteIndex       int
}

// AutocompleteContext determines what type of suggestions to show
type AutocompleteContext int

const (
	// AutocompleteContextField suggests field names
	AutocompleteContextField AutocompleteContext = iota
	// AutocompleteContextOperator suggests operators after field name
	AutocompleteContextOperator
	// AutocompleteContextValue suggests field values
	AutocompleteContextValue
	// AutocompleteContextVariable suggests $variables
	AutocompleteContextVariable
	// AutocompleteContextOption suggests backend options (e.g., index, namespace)
	AutocompleteContextOption
)

// Suggestion represents an autocomplete option
type Suggestion struct {
	Text        string              // The suggestion text
	Description string              // Optional description
	Context     AutocompleteContext // What type of suggestion this is
}

// NewChipSearchState creates an initialized ChipSearchState
func NewChipSearchState() ChipSearchState {
	return ChipSearchState{
		Chips:                   make([]Chip, 0),
		CurrentInput:            "",
		CursorPosition:          0,
		SelectedChip:            -1, // -1 means typing in input
		AutocompleteOpen:        false,
		AutocompleteSuggestions: make([]Suggestion, 0),
		AutocompleteIndex:       0,
	}
}

// AddChip adds a new chip to the state
func (s *ChipSearchState) AddChip(chip Chip) {
	s.Chips = append(s.Chips, chip)
	s.CurrentInput = ""
	s.CursorPosition = 0
	s.SelectedChip = -1
}

// RemoveChip removes the chip at the given index
func (s *ChipSearchState) RemoveChip(index int) {
	if index >= 0 && index < len(s.Chips) {
		s.Chips = append(s.Chips[:index], s.Chips[index+1:]...)
		s.SelectedChip = -1
	}
}

// RemoveLastChip removes the last chip if any exist
func (s *ChipSearchState) RemoveLastChip() bool {
	if len(s.Chips) > 0 {
		s.Chips = s.Chips[:len(s.Chips)-1]
		return true
	}
	return false
}

// Clear removes all chips and resets state
func (s *ChipSearchState) Clear() {
	s.Chips = make([]Chip, 0)
	s.CurrentInput = ""
	s.CursorPosition = 0
	s.SelectedChip = -1
	s.AutocompleteOpen = false
	s.AutocompleteSuggestions = make([]Suggestion, 0)
	s.AutocompleteIndex = 0
}

// IsEmpty returns true if there are no chips and no current input
func (s *ChipSearchState) IsEmpty() bool {
	return len(s.Chips) == 0 && s.CurrentInput == ""
}
