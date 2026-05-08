package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
	"github.com/estran-studio/logviewer/pkg/ty"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// --- Advanced Mocks ---

type MockClientFactory struct{}

func (m *MockClientFactory) Get(_ string) (*client.LogBackend, error) {
	return nil, nil
}

type InMemoryLogStore struct {
	Entries map[string][]client.LogEntry
}

func NewInMemoryLogStore() *InMemoryLogStore {
	return &InMemoryLogStore{
		Entries: make(map[string][]client.LogEntry),
	}
}

func (s *InMemoryLogStore) AddEntries(contextID string, entries []client.LogEntry) {
	s.Entries[contextID] = append(s.Entries[contextID], entries...)
}

type MockSearchFactory struct {
	Store *InMemoryLogStore
}

func (m *MockSearchFactory) GetSearchResult(_ context.Context, contextID string, _ []string, logSearch client.LogSearch, _ map[string]string) (client.LogSearchResult, error) {
	allEntries, ok := m.Store.Entries[contextID]
	if !ok {
		allEntries = []client.LogEntry{}
	}

	return &InMemoryLogResult{
		AllEntries: allEntries,
		Search:     &logSearch,
	}, nil
}

func (m *MockSearchFactory) GetSearchContext(_ context.Context, _ string, _ []string, _ client.LogSearch, _ map[string]string) (*config.SearchContext, error) {
	return &config.SearchContext{}, nil
}

func (m *MockSearchFactory) GetFieldValues(_ context.Context, contextID string, _ []string, _ client.LogSearch, fields []string, _ map[string]string) (map[string][]string, error) {
	entries, ok := m.Store.Entries[contextID]
	if !ok {
		return map[string][]string{}, nil
	}

	uniSet := make(ty.UniSet[string])
	for _, entry := range entries {
		for _, field := range fields {
			if val, ok := entry.Fields[field]; ok {
				if strVal, ok := val.(string); ok {
					uniSet.Add(field, strVal)
				}
			}
		}
	}

	return map[string][]string(uniSet), nil
}

type InMemoryLogResult struct {
	AllEntries []client.LogEntry
	Search     *client.LogSearch
}

func (m *InMemoryLogResult) GetSearch() *client.LogSearch {
	return m.Search
}

func (m *InMemoryLogResult) GetEntries(_ context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	var filtered []client.LogEntry

	// Use GetEffectiveFilter to combine legacy Fields and new AST Filter
	filter := m.Search.GetEffectiveFilter()

	// Apply Filters
	for _, entry := range m.AllEntries {
		matches := true

		// 1. AST Filter
		if filter != nil {
			if !matchFilterRecursive(entry, filter) {
				matches = false
			}
		}

		// 2. Filter by NativeQuery (Simple partial match on message)
		if matches && m.Search.NativeQuery.Set && m.Search.NativeQuery.Value != "" {
			query := m.Search.NativeQuery.Value

			// Handle "field:value" simple syntax from TUI search bar
			parts := bytes.Split([]byte(query), []byte(":"))
			if len(parts) == 2 {
				// handled by Fields/Filter above if correctly parsed
				continue
			} else if !bytes.Contains([]byte(entry.Message), []byte(query)) {
				matches = false
			}
		}

		if matches {
			filtered = append(filtered, entry)
		}
	}

	// Pagination
	pageSize := 10 // Default page size for test
	if m.Search.Size.Set && m.Search.Size.Value > 0 {
		pageSize = m.Search.Size.Value
	}

	offset := 0
	if m.Search.PageToken.Set && m.Search.PageToken.Value != "" {
		_, _ = fmt.Sscanf(m.Search.PageToken.Value, "offset-%d", &offset)
	}

	startIdx := offset
	endIdx := startIdx + pageSize
	if endIdx > len(filtered) {
		endIdx = len(filtered)
	}

	// Safety check
	if startIdx >= len(filtered) {
		return []client.LogEntry{}, nil, nil
	}
	result := filtered[startIdx:endIdx]

	return result, nil, nil
}

func matchFilterRecursive(entry client.LogEntry, f *client.Filter) bool {
	if f == nil {
		return true
	}

	// Branch node
	if f.Logic != "" {
		if f.Logic == client.LogicAnd {
			if len(f.Filters) == 0 {
				return true
			}
			for _, sub := range f.Filters {
				if !matchFilterRecursive(entry, &sub) {
					return false
				}
			}
			return true
		}
		if f.Logic == client.LogicOr {
			if len(f.Filters) == 0 {
				return false
			}
			for _, sub := range f.Filters {
				if matchFilterRecursive(entry, &sub) {
					return true
				}
			}
			return false // None matched
		}
		if f.Logic == client.LogicNot {
			for _, sub := range f.Filters {
				if matchFilterRecursive(entry, &sub) {
					return false
				}
			}
			return true
		}
	}

	// Leaf node
	if f.Field == "" {
		return true // Unknown/empty filter
	}

	// Get value from entry
	var strVal string
	val, ok := entry.Fields[f.Field]

	// Handle special fields
	switch f.Field {
	case "level":
		strVal = entry.Level
		ok = true
	case "message":
		strVal = entry.Message
		ok = true
	default:
		if ok {
			strVal, _ = val.(string)
		}
	}

	match := false
	if !ok {
		// Field missing
		match = false
	} else {
		switch f.Op {
		case operator.Equals, "":
			match = (strVal == f.Value)
		case operator.Match, operator.Regex: // Simple contains for test
			match = bytes.Contains([]byte(strVal), []byte(f.Value))
		default:
			// Fallback to equals for test
			match = (strVal == f.Value)
		}
	}

	if f.Negate {
		return !match
	}
	return match
}

func (m *InMemoryLogResult) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	fields := make(ty.UniSet[string])
	for _, entry := range m.AllEntries {
		for k, v := range entry.Fields {
			if s, ok := v.(string); ok {
				fields.Add(k, s)
			}
		}
	}
	return fields, nil, nil
}

func (m *InMemoryLogResult) GetPaginationInfo() *client.PaginationInfo {
	// Re-calculate filtering to determine if more exist (simplified)
	var filtered []client.LogEntry
	filter := m.Search.GetEffectiveFilter()
	for _, entry := range m.AllEntries {
		matches := true
		if filter != nil && !matchFilterRecursive(entry, filter) {
			matches = false
		}
		if matches && m.Search.NativeQuery.Set && m.Search.NativeQuery.Value != "" {
			if !bytes.Contains([]byte(entry.Message), []byte(m.Search.NativeQuery.Value)) {
				matches = false
			}
		}
		if matches {
			filtered = append(filtered, entry)
		}
	}

	offset := 0
	if m.Search.PageToken.Set && m.Search.PageToken.Value != "" {
		_, _ = fmt.Sscanf(m.Search.PageToken.Value, "offset-%d", &offset)
	}

	pageSize := 10
	if m.Search.Size.Set && m.Search.Size.Value > 0 {
		pageSize = m.Search.Size.Value
	}

	hasMore := (offset + pageSize) < len(filtered)
	nextToken := ""
	if hasMore {
		nextToken = fmt.Sprintf("offset-%d", offset+pageSize)
	}

	return &client.PaginationInfo{
		HasMore:       hasMore,
		NextPageToken: nextToken,
	}
}

func (m *InMemoryLogResult) Err() <-chan error {
	return nil
}

// --- Integration Test Infrastructure ---

func waitForCondition(t *testing.T, tm *teatest.TestModel, condition func([]byte) bool, msg ...string) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		// Read output from the virtual terminal
		out, err := io.ReadAll(tm.Output())
		if err != nil {
			t.Logf("Error reading output: %v", err)
		} else if condition(out) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	// One last check
	out, _ := io.ReadAll(tm.Output())
	if !condition(out) {
		failMsg := "Timeout waiting for condition"
		if len(msg) > 0 {
			failMsg = msg[0]
		}
		t.Errorf("%s. Last output:\n%s", failMsg, string(out))
	}
}

type TestStep struct {
	Name          string
	Action        func(tm *teatest.TestModel)
	ExpectPresent []string
	ExpectAbsent  []string
}

func RunScenario(t *testing.T, tm *teatest.TestModel, steps []TestStep) {
	for i, step := range steps {
		t.Logf(">> Running Step %d: %s", i+1, step.Name)

		if step.Action != nil {
			step.Action(tm)
		}

		// Validation with retry/timeout
		condition := func(bts []byte) bool {
			// Check presence
			for _, s := range step.ExpectPresent {
				if !bytes.Contains(bts, []byte(s)) {
					return false
				}
			}
			// Check absence
			for _, s := range step.ExpectAbsent {
				if bytes.Contains(bts, []byte(s)) {
					return false
				}
			}
			return true
		}

		waitForCondition(t, tm, condition, fmt.Sprintf("Step %d (%s) validation failed", i+1, step.Name))
	}
}

func TestTUI_ComplexInteraction(t *testing.T) {
	// 1. Setup Data & Mocks
	store := NewInMemoryLogStore()

	// Generate some logs with mixed fields
	var entries []client.LogEntry
	for i := 0; i < 20; i++ {
		entry := client.LogEntry{
			Timestamp: time.Now().Add(time.Duration(-i) * time.Minute),
			ContextID: "prod",
			Fields:    make(ty.MI),
		}

		if i%2 == 0 {
			entry.Level = "INFO"
			entry.Message = "User logged in " + string(rune(i+65)) // A, C, E...
			entry.Fields["level"] = "INFO"
			entry.Fields["user"] = "admin"
		} else {
			entry.Level = "ERROR"
			entry.Message = "Service timeout " + string(rune(i+65)) // B, D, F...
			entry.Fields["level"] = "ERROR"
			entry.Fields["service"] = "auth"
		}
		entries = append(entries, entry)
	}
	store.AddEntries("prod", entries)

	searchFactory := &MockSearchFactory{Store: store}

	cfg := &config.ContextConfig{
		Contexts: config.Contexts{
			"prod": {},
		},
	}

	// 2. Initialize the Model
	model := New(cfg, &MockClientFactory{}, searchFactory)
	model.InitialContexts = []string{"prod"}

	// 3. Start Teatest harness
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(80, 20))

	// 4. Run Scenario
	steps := []TestStep{
		{
			Name:   "1. Initial Load - Verify Context and All Logs",
			Action: nil, // Just wait for initial load
			ExpectPresent: []string{
				"[prod]",          // Context visible
				"User logged in",  // INFO visible
				"Service timeout", // ERROR visible
			},
			ExpectAbsent: []string{},
		},
		{
			Name: "2. Filter by Level=ERROR",
			Action: func(tm *teatest.TestModel) {
				tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}) // Open Search
				// Wait briefly for focus
				waitForCondition(t, tm, func(b []byte) bool { return bytes.Contains(b, []byte(">")) })

				tm.Type("level=ERROR")
				tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
			},
			ExpectPresent: []string{
				"Service timeout", // ERROR logs visible
				"level=ERROR",     // Chip visible
			},
			ExpectAbsent: []string{
				"User logged in", // INFO logs hidden
			},
		},
		{
			Name: "3. Remove Filter",
			Action: func(tm *teatest.TestModel) {
				tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}) // Open Search (Focus)
				waitForCondition(t, tm, func(b []byte) bool { return bytes.Contains(b, []byte("level=ERROR")) })

				// Delete the chip (assuming it's selected or we clean input)
				// Search bar logic: if input empty, backspace removes last chip
				tm.Send(tea.KeyMsg{Type: tea.KeyBackspace})

				// Must press Enter to re-submit/refresh with no filters
				tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
			},
			ExpectPresent: []string{
				"User logged in",  // INFO returned
				"Service timeout", // ERROR still there
			},
			ExpectAbsent: []string{
				"level=ERROR", // Chip gone
			},
		},
	}

	RunScenario(t, tm, steps)
	_ = tm.Quit()
}

func TestTUI_Integration_Workflow(t *testing.T) {
	// 1. Setup Data & Mocks
	store := NewInMemoryLogStore()

	// Generate some logs
	var entries []client.LogEntry
	for i := 0; i < 20; i++ {
		entry := client.LogEntry{
			Timestamp: time.Now().Add(time.Duration(-i) * time.Minute),
			ContextID: "prod",
			Fields:    make(ty.MI),
		}

		if i%2 == 0 {
			entry.Level = "INFO"
			entry.Message = "Application started successfully " + string(rune(i))
			entry.Fields["level"] = "INFO"
		} else {
			entry.Level = "ERROR"
			entry.Message = "Database connection failed " + string(rune(i))
			entry.Fields["level"] = "ERROR"
		}
		entries = append(entries, entry)
	}
	store.AddEntries("prod", entries)

	searchFactory := &MockSearchFactory{Store: store}

	cfg := &config.ContextConfig{
		Contexts: config.Contexts{
			"prod": {},
		},
	}

	// 2. Initialize the Model
	// We start with "prod" context which matches our mock data
	model := New(cfg, &MockClientFactory{}, searchFactory)
	model.InitialContexts = []string{"prod"}

	// 3. Start Teatest harness
	// This runs the model in a virtual terminal (VT)
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(80, 20))

	// 4. Assert Initial State
	// Wait until the view renders our log message
	waitForCondition(t, tm, func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Application started successfully"))
	})

	// 5. Interaction: Open Search Bar
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}) // Press '/'

	// Assert we are in search mode (Search bar should be focused/visible)
	// We look for the prompt or specific UI element
	waitForCondition(t, tm, func(bts []byte) bool {
		return bytes.Contains(bts, []byte(">")) // Looking for prompt character
	})

	// 6. Interaction: Type a filter and press Enter
	// Entering "level=ERROR" which the search bar chips parser handles
	tm.Type("level=ERROR")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// 7. Validation
	// This should filter to only ERROR logs
	waitForCondition(t, tm, func(bts []byte) bool {
		// Check if we see ERROR logs
		hasError := bytes.Contains(bts, []byte("Database connection failed"))
		// Check if we DO NOT see INFO logs
		hasInfo := bytes.Contains(bts, []byte("Application started successfully"))

		return hasError && !hasInfo
	})

	// Cleanup
	_ = tm.Quit()
}
