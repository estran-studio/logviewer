// SPDX-License-Identifier: GPL-3.0-only

// Package tui provides the terminal user interface components.
package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/TylerBrock/colorjson"
	"github.com/atotto/clipboard"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/log/factory"
	"github.com/estran-studio/logviewer/pkg/log/printer"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FocusMode represents which component has focus
type FocusMode int

const (
	// FocusList means the main log list has focus.
	FocusList FocusMode = iota
	// FocusSearch means the search bar has focus.
	FocusSearch
	// FocusSidebar means the sidebar has focus.
	FocusSidebar
	// FocusContextSelect means the context selection menu has focus.
	FocusContextSelect
	// FocusInheritSelect means the inherit selection menu has focus.
	FocusInheritSelect
	// FocusConfirmation means a confirmation dialog has focus.
	FocusConfirmation
)

// ConfirmationType represents what we are confirming
type ConfirmationType int

const (
	// ConfirmCloseTab means we are confirming closing a tab.
	ConfirmCloseTab ConfirmationType = iota
	// ConfirmQuitApp means we are confirming quitting the application.
	ConfirmQuitApp
)

// SidebarMode represents what content the sidebar displays
type SidebarMode int

const (
	// SidebarModeEntry shows selected entry details.
	SidebarModeEntry SidebarMode = iota // Show selected entry details
	// SidebarModeFields shows the list of available fields.
	SidebarModeFields // Show global fields with values
	// SidebarModeJSON shows the log entry as formatted JSON.
	SidebarModeJSON // Show formatted JSON from selected entry
)

// Tab represents an open context/query tab
type Tab struct {
	ID         string
	Name       string
	ContextID  string
	Entries    []client.LogEntry
	Cursor     int
	ViewOffset int
	Search     *client.LogSearch
	Inherits   []string // Search templates to inherit
	Result     client.LogSearchResult
	Template   *template.Template // Printer template for formatting entries
	Fields     ty.UniSet[string]  // Available fields with their values from GetFields()
	Loading    bool
	Error      error
	StreamChan <-chan []client.LogEntry // For live streaming
	ErrorChan  <-chan error             // For async errors from backend
	CancelFunc context.CancelFunc
	ClientType string // Backend client type (e.g. splunk, opensearch)

	// Per-tab search bar state
	SearchState        ChipSearchState     // The chips and input state for this tab
	AvailableFields    []string            // Fields discovered from loaded entries
	AvailableVariables []string            // Variables from config
	VariableMetadata   map[string]string   // Variable name -> description
	FieldValues        map[string][]string // Field -> possible values (cached)

	// JSON detection cache
	JSONCache map[string][]string // Maps message hash -> detected JSON strings

	// Pagination state
	PaginationInfo *client.PaginationInfo // Pagination info from last search
	LoadingMore    bool                   // True when fetching more pages
}

// LogEntryMsg is sent when new log entries arrive
type LogEntryMsg struct {
	TabID          string
	Entries        []client.LogEntry
	Result         client.LogSearchResult   // The search result (for printer config)
	Template       *template.Template       // Compiled printer template
	Fields         ty.UniSet[string]        // Available fields with values from GetFields()
	StreamChan     <-chan []client.LogEntry // For live streaming (if applicable)
	ErrorChan      <-chan error             // For async errors from backend
	PaginationInfo *client.PaginationInfo   // Pagination info (HasMore, NextPageToken)
	IsPagination   bool                     // True if this is a pagination response (prepend instead of append)
}

// StreamBatchMsg delivers streamed log entries
type StreamBatchMsg struct {
	TabID   string
	Entries []client.LogEntry
}

// ErrorMsg is sent when an error occurs
type ErrorMsg struct {
	TabID string
	Err   error
}

// LoadingMsg indicates loading state change
type LoadingMsg struct {
	TabID   string
	Loading bool
}

// AddTabMsg requests adding a new tab
type AddTabMsg struct {
	ContextID string
	Search    *client.LogSearch
}

// InitMsg is sent to trigger initial tab loading
type InitMsg struct{}

// ClearStatusMsg is sent to clear status messages
type ClearStatusMsg struct{}

// Model is the main TUI state
type Model struct {
	// Window dimensions
	Width  int
	Height int

	// Tabs
	Tabs      []*Tab
	ActiveTab int

	// UI State
	Focus          FocusMode
	Confirmation   ConfirmationType
	DetailsVisible bool
	SidebarMode    SidebarMode // Entry details or Global fields
	SplitRatio     float64     // 0.0 to 1.0, ratio for log list
	ShowHelp       bool
	LineWrapping   bool // Enable/disable line wrapping for multiline logs

	// Context selection state (for Ctrl+T new tab)
	AvailableContexts []string
	ContextCursor     int

	// Inherit selection state (for I key)
	AvailableSearches []string        // Search template names from config
	ActiveSearches    map[string]bool // Currently active inherited searches
	InheritCursor     int             // Cursor for inherit selection

	// Components
	SearchBar SearchBar
	StatusBar StatusBar
	Viewport  viewport.Model
	SidebarVP viewport.Model

	// Styling
	Styles Styles
	Keys   KeyMap

	// Config
	Config        *config.ContextConfig
	ClientFactory factory.LogBackendFactory
	SearchFactory factory.SearchFactory

	// Runtime
	RuntimeVars map[string]string

	// Initial contexts to load (set before Init)
	InitialContexts []string
	InitialSearch   *client.LogSearch
	InitialInherits []string
}

// New creates a new TUI model
func New(cfg *config.ContextConfig, clientFactory factory.LogBackendFactory, searchFactory factory.SearchFactory) Model {
	vp := viewport.New(80, 20)
	vp.SetContent("")

	sbvp := viewport.New(30, 20)
	sbvp.SetContent("")

	// Collect available contexts and searches
	var contexts []string
	var searches []string
	if cfg != nil {
		for id := range cfg.Contexts {
			contexts = append(contexts, id)
		}
		for id := range cfg.Searches {
			searches = append(searches, id)
		}
	}
	sort.Strings(contexts)
	sort.Strings(searches)

	// Create search bar and status bar
	searchBar := NewSearchBar()
	statusBar := NewStatusBar()

	return Model{
		Width:             80,
		Height:            24,
		Tabs:              make([]*Tab, 0),
		ActiveTab:         0,
		Focus:             FocusList,
		DetailsVisible:    false,
		SplitRatio:        0.7,
		ShowHelp:          false,
		LineWrapping:      false,
		AvailableContexts: contexts,
		ContextCursor:     0,
		AvailableSearches: searches,
		ActiveSearches:    make(map[string]bool),
		InheritCursor:     0,
		SearchBar:         searchBar,
		StatusBar:         statusBar,
		Viewport:          vp,
		SidebarVP:         sbvp,
		Styles:            DefaultStyles(),
		Keys:              DefaultKeyMap(),
		Config:            cfg,
		ClientFactory:     clientFactory,
		SearchFactory:     searchFactory,
		RuntimeVars:       make(map[string]string),
	}
}

// Init initializes the TUI
func (m Model) Init() tea.Cmd {
	log.Printf("[DEBUG] TUI Init called, initialContexts=%v", m.InitialContexts)
	return func() tea.Msg { return InitMsg{} }
}

// addTabCmd creates a tab and returns a command to load its logs
func (m *Model) addTabCmd(contextID string, search *client.LogSearch) tea.Cmd {
	name := contextID
	if name == "" {
		name = fmt.Sprintf("Tab %d", len(m.Tabs)+1)
	}

	// Resolve context config to get default search params
	var contextSearch *client.LogSearch
	var clientType string
	if m.Config != nil && contextID != "" {
		if ctxConfig, ok := m.Config.Contexts[contextID]; ok {
			// Resolve client type
			if clientCfg, ok := m.Config.Clients[ctxConfig.Client]; ok {
				clientType = clientCfg.Type
			}

			// Deep copy the search from config
			if searchCtx, err := m.Config.GetSearchContext(contextID, nil, client.LogSearch{}, nil); err == nil {
				contextSearch = &searchCtx.Search
			} else {
				log.Printf("[WARN] Failed to resolve context config for %s: %v", contextID, err)
			}
		}
	}

	tab := &Tab{
		ID:                 fmt.Sprintf("tab-%d-%d", len(m.Tabs), time.Now().UnixNano()),
		Name:               name,
		ContextID:          contextID,
		Entries:            make([]client.LogEntry, 0),
		Cursor:             0,
		Search:             search,
		Inherits:           m.InitialInherits, // Set inherits from model before loading logs
		Loading:            true,
		SearchState:        NewChipSearchState(),
		AvailableFields:    make([]string, 0),
		AvailableVariables: make([]string, 0),
		VariableMetadata:   make(map[string]string),
		FieldValues:        make(map[string][]string),
		JSONCache:          make(map[string][]string),
		ClientType:         clientType,
	}

	// Populate search bar state for this tab
	tempSB := NewSearchBar()
	tempSB.ClientType = clientType

	// 1. Add Context chip (informational)
	if contextID != "" {
		tempSB.State.Chips = append(tempSB.State.Chips, Chip{
			Type:    ChipTypeContext,
			Value:   contextID,
			Display: contextID,
		})
	}

	// 2. Prepare effective search by merging context defaults and explicit search overrides
	effectiveSearch := &client.LogSearch{}
	if contextSearch != nil {
		// Start with context defaults (contextSearch is already a deep copy from GetSearchContext)
		*effectiveSearch = *contextSearch
	}
	if search != nil {
		// Merge overrides (CLI args / manual search)
		if err := effectiveSearch.MergeInto(search); err != nil {
			log.Printf("[ERROR] Failed to merge search overrides: %v", err)
		}
	}

	// 1. Populate from merged search (handles context + CLI overrides without duplicates)
	tempSB.PopulateFromSearch(effectiveSearch)

	// 2. Add inherit chips
	for _, inherit := range tab.Inherits {
		tempSB.State.Chips = append(tempSB.State.Chips, Chip{
			Type:    ChipTypeInherit,
			Value:   inherit,
			Display: "inherit:" + inherit,
		})
	}

	tab.SearchState = tempSB.State

	m.Tabs = append(m.Tabs, tab)
	m.ActiveTab = len(m.Tabs) - 1

	// Restore search bar from the new tab
	m.restoreSearchBarFromTab(tab)
	m.StatusBar.UpdateFromTab(tab)
	m.StatusBar.UpdateTimeRangeFromChips(m.SearchBar.State.Chips)

	log.Printf("[DEBUG] TUI addTabCmd: created tab, tabID=%s, contextID=%s, inherits=%v, totalTabs=%d", tab.ID, contextID, tab.Inherits, len(m.Tabs))
	return m.loadTabLogsCmd(tab)
}

// CurrentTab returns the currently active tab or nil
func (m *Model) CurrentTab() *Tab {
	if len(m.Tabs) == 0 || m.ActiveTab >= len(m.Tabs) {
		return nil
	}
	return m.Tabs[m.ActiveTab]
}

// saveSearchBarToTab saves the current search bar state to the given tab
func (m *Model) saveSearchBarToTab(tab *Tab) {
	if tab == nil {
		return
	}
	tab.SearchState = m.SearchBar.State
	tab.AvailableFields = m.SearchBar.AvailableFields
	tab.AvailableVariables = m.SearchBar.AvailableVariables
	tab.VariableMetadata = m.SearchBar.VariableMetadata
	tab.FieldValues = m.SearchBar.FieldValues
	// ClientType is static per tab, usually no need to save back,
	// but if we allowed changing client type dynamically, we would.
}

// restoreSearchBarFromTab restores the search bar state from the given tab
func (m *Model) restoreSearchBarFromTab(tab *Tab) {
	if tab == nil {
		return
	}
	m.SearchBar.State = tab.SearchState
	m.SearchBar.AvailableFields = tab.AvailableFields
	m.SearchBar.AvailableVariables = tab.AvailableVariables
	m.SearchBar.VariableMetadata = tab.VariableMetadata
	m.SearchBar.FieldValues = tab.FieldValues
	m.SearchBar.ClientType = tab.ClientType
	// Sync the text input with the restored state
	m.SearchBar.TextInput.SetValue(tab.SearchState.CurrentInput)
}

// switchToTab saves state from current tab and switches to target tab
func (m *Model) switchToTab(newIndex int) {
	if newIndex < 0 || newIndex >= len(m.Tabs) {
		return
	}
	// Save current tab's search bar state
	m.saveSearchBarToTab(m.CurrentTab())
	// Switch tab
	m.ActiveTab = newIndex
	// Restore new tab's search bar state
	m.restoreSearchBarFromTab(m.CurrentTab())
	// Update status bar and viewport
	m.StatusBar.UpdateFromTab(m.CurrentTab())
	m.StatusBar.UpdateTimeRangeFromChips(m.SearchBar.State.Chips)
	m.updateViewportContent()
}

// AddTab creates a new tab with the given context (for use from Update)
func (m *Model) AddTab(contextID string, search *client.LogSearch) tea.Cmd {
	return m.addTabCmd(contextID, search)
}

// loadTabLogsCmd starts loading logs for a tab
func (m *Model) loadTabLogsCmd(tab *Tab) tea.Cmd {
	// Capture values needed by the closure (not pointers to stack-allocated model)
	searchFactory := m.SearchFactory
	runtimeVars := m.RuntimeVars
	tabID := tab.ID
	contextID := tab.ContextID
	search := tab.Search
	inherits := tab.Inherits

	log.Printf("[DEBUG] TUI loadTabLogsCmd: preparing command, tabID=%s, contextID=%s, inherits=%v", tabID, contextID, inherits)

	return func() tea.Msg {
		log.Printf("[DEBUG] TUI loadTabLogsCmd: executing, tabID=%s", tabID)
		if searchFactory == nil {
			log.Printf("[ERROR] TUI loadTabLogsCmd: no search factory")
			return ErrorMsg{TabID: tabID, Err: fmt.Errorf("no search factory configured")}
		}

		ctx, cancel := context.WithCancel(context.Background())
		tab.CancelFunc = cancel

		if search == nil {
			search = &client.LogSearch{}
		}

		log.Printf("[DEBUG] TUI loadTabLogsCmd: calling GetSearchResult, tabID=%s, inherits=%v", tabID, inherits)
		result, err := searchFactory.GetSearchResult(ctx, contextID, inherits, *search, runtimeVars)
		if err != nil {
			log.Printf("[ERROR] TUI loadTabLogsCmd: GetSearchResult failed, tabID=%s, error=%v", tabID, err)
			return ErrorMsg{TabID: tabID, Err: err}
		}

		tab.Result = result

		// Compile the printer template from the search result
		printerOptions := result.GetSearch().PrinterOptions
		templateConfig := printerOptions.Template
		if templateConfig.Value == "" {
			// Default template includes message
			templateConfig.S("[{{FormatTimestamp .Timestamp \"15:04:05\"}}] [{{.ContextID}}] {{.Level}} {{.Message}}")
		}

		tmpl, tmplErr := template.New("tui_printer").Funcs(printer.GetTemplateFunctionsMap()).Parse(templateConfig.Value)
		if tmplErr != nil {
			log.Printf("[WARN] TUI loadTabLogsCmd: failed to parse template: %v, using default", tmplErr)
			tmpl, _ = template.New("tui_printer").Funcs(printer.GetTemplateFunctionsMap()).Parse("[{{FormatTimestamp .Timestamp \"15:04:05\"}}] [{{.ContextID}}] {{.Level}} {{.Message}}")
		}

		log.Printf("[DEBUG] TUI loadTabLogsCmd: calling GetEntries, tabID=%s", tabID)
		entries, entryChan, err := result.GetEntries(ctx)
		if err != nil {
			log.Printf("[ERROR] TUI loadTabLogsCmd: GetEntries failed, tabID=%s, error=%v", tabID, err)
			return ErrorMsg{TabID: tabID, Err: err}
		}

		// Extract JSON fields from entries
		searchConfig := result.GetSearch()
		for i := range entries {
			client.ExtractJSONFromEntry(&entries[i], searchConfig)
		}

		log.Printf("[DEBUG] TUI loadTabLogsCmd: got entries, tabID=%s, count=%d", tabID, len(entries))

		// Get available fields for global fields view and autocomplete
		var fields ty.UniSet[string]
		if initialFields, _, err := result.GetFields(ctx); err == nil {
			fields = initialFields
			log.Printf("[DEBUG] TUI loadTabLogsCmd: got fields, tabID=%s, count=%d", tabID, len(fields))
		} else {
			log.Printf("[WARN] TUI loadTabLogsCmd: GetFields failed: %v", err)
		}

		// Get pagination info
		paginationInfo := result.GetPaginationInfo()

		// Return initial entries with template, fields, streaming channel, and error channel
		msg := LogEntryMsg{
			TabID:          tabID,
			Entries:        entries,
			Result:         result,
			Template:       tmpl,
			Fields:         fields,
			StreamChan:     entryChan,    // Will be handled by Update loop via subscription
			ErrorChan:      result.Err(), // Monitor for async errors from backend
			PaginationInfo: paginationInfo,
			IsPagination:   false, // Initial load, not pagination
		}

		return msg
	}
}

// loadMoreLogsCmd fetches the next page of logs using pagination token
func (m *Model) loadMoreLogsCmd(tab *Tab) tea.Cmd {
	// Capture values needed by the closure
	searchFactory := m.SearchFactory
	runtimeVars := m.RuntimeVars
	tabID := tab.ID
	contextID := tab.ContextID
	inherits := tab.Inherits

	// Get the next page token
	if tab.PaginationInfo == nil || !tab.PaginationInfo.HasMore {
		log.Printf("[DEBUG] TUI loadMoreLogsCmd: no more pages, tabID=%s", tabID)
		return nil
	}

	nextPageToken := tab.PaginationInfo.NextPageToken
	if nextPageToken == "" {
		log.Printf("[DEBUG] TUI loadMoreLogsCmd: empty page token, tabID=%s", tabID)
		return nil
	}

	// Create a new search with the page token
	search := &client.LogSearch{}
	if tab.Search != nil {
		*search = *tab.Search // Copy current search
	}
	search.PageToken.S(nextPageToken)

	log.Printf("[DEBUG] TUI loadMoreLogsCmd: fetching next page, tabID=%s, pageToken=%s", tabID, nextPageToken)

	return func() tea.Msg {
		log.Printf("[DEBUG] TUI loadMoreLogsCmd: executing, tabID=%s", tabID)
		if searchFactory == nil {
			log.Printf("[ERROR] TUI loadMoreLogsCmd: no search factory")
			return ErrorMsg{TabID: tabID, Err: fmt.Errorf("no search factory configured")}
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		log.Printf("[DEBUG] TUI loadMoreLogsCmd: calling GetSearchResult, tabID=%s, pageToken=%s", tabID, nextPageToken)
		result, err := searchFactory.GetSearchResult(ctx, contextID, inherits, *search, runtimeVars)
		if err != nil {
			log.Printf("[ERROR] TUI loadMoreLogsCmd: GetSearchResult failed, tabID=%s, error=%v", tabID, err)
			return ErrorMsg{TabID: tabID, Err: err}
		}

		// Compile the printer template from the search result
		printerOptions := result.GetSearch().PrinterOptions
		templateConfig := printerOptions.Template
		if templateConfig.Value == "" {
			templateConfig.S("[{{FormatTimestamp .Timestamp \"15:04:05\"}}] [{{.ContextID}}] {{.Level}} {{.Message}}")
		}

		tmpl, tmplErr := template.New("tui_printer").Funcs(printer.GetTemplateFunctionsMap()).Parse(templateConfig.Value)
		if tmplErr != nil {
			log.Printf("[WARN] TUI loadMoreLogsCmd: failed to parse template: %v, using default", tmplErr)
			tmpl, _ = template.New("tui_printer").Funcs(printer.GetTemplateFunctionsMap()).Parse("[{{FormatTimestamp .Timestamp \"15:04:05\"}}] [{{.ContextID}}] {{.Level}} {{.Message}}")
		}

		log.Printf("[DEBUG] TUI loadMoreLogsCmd: calling GetEntries, tabID=%s", tabID)
		entries, _, err := result.GetEntries(ctx)
		if err != nil {
			log.Printf("[ERROR] TUI loadMoreLogsCmd: GetEntries failed, tabID=%s, error=%v", tabID, err)
			return ErrorMsg{TabID: tabID, Err: err}
		}

		// Extract JSON fields from entries
		searchConfig := result.GetSearch()
		for i := range entries {
			client.ExtractJSONFromEntry(&entries[i], searchConfig)
		}

		log.Printf("[DEBUG] TUI loadMoreLogsCmd: got entries, tabID=%s, count=%d", tabID, len(entries))

		// Get pagination info for next page
		paginationInfo := result.GetPaginationInfo()

		// Return paginated entries
		msg := LogEntryMsg{
			TabID:          tabID,
			Entries:        entries,
			Result:         result,
			Template:       tmpl,
			PaginationInfo: paginationInfo,
			IsPagination:   true, // This is a pagination response - prepend entries
		}

		return msg
	}
}

// waitForStreamBatch subscribes to a streaming channel and returns the next batch
// This follows the Bubble Tea message-passing pattern for safe concurrent updates
func waitForStreamBatch(tab *Tab) tea.Cmd {
	return func() tea.Msg {
		if tab.StreamChan == nil {
			return nil
		}

		entries, ok := <-tab.StreamChan
		if !ok {
			// Channel closed - stop streaming
			return LoadingMsg{TabID: tab.ID, Loading: false}
		}

		// Extract JSON fields from streamed entries
		searchConfig := tab.Result.GetSearch()
		for i := range entries {
			client.ExtractJSONFromEntry(&entries[i], searchConfig)
		}

		return StreamBatchMsg{TabID: tab.ID, Entries: entries}
	}
}

// waitForError subscribes to an error channel and returns any backend errors
// This follows the Bubble Tea message-passing pattern for safe concurrent updates
func waitForError(tab *Tab) tea.Cmd {
	return func() tea.Msg {
		if tab.ErrorChan == nil {
			return nil
		}

		err, ok := <-tab.ErrorChan
		if !ok {
			// Channel closed - no more errors
			return nil
		}

		log.Printf("[ERROR] TUI waitForError: received error from backend, tabID=%s, error=%v", tab.ID, err)
		return ErrorMsg{TabID: tab.ID, Err: err}
	}
}

// Update handles messages and updates the model
//
//nolint:gocyclo // TUI message handler with many message types
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.updateViewportSizes()
		m.updateViewportContent()
		m.updateSidebarContent()

	case tea.KeyMsg:
		// Handle search input mode separately
		if m.Focus == FocusSearch {
			return m.handleSearchInput(msg)
		}
		// Handle inherit selection mode
		if m.Focus == FocusInheritSelect {
			return m.handleInheritSelect(msg)
		}
		// Handle confirmation mode
		if m.Focus == FocusConfirmation {
			return m.handleConfirmation(msg)
		}
		// Handle context selection mode
		if m.Focus == FocusContextSelect {
			return m.handleContextSelect(msg)
		}
		return m.handleKeyPress(msg)

	case LogEntryMsg:
		// Update the tab with new entries
		log.Printf("[DEBUG] TUI LogEntryMsg received, tabID=%s, entries=%d, isPagination=%v, currentTabs=%d", msg.TabID, len(msg.Entries), msg.IsPagination, len(m.Tabs))
		found := false
		for _, tab := range m.Tabs {
			if tab.ID == msg.TabID {
				// Handle pagination (prepend) vs normal (append)
				if msg.IsPagination {
					// Prepend new entries to the beginning (older logs)
					oldCursor := tab.Cursor
					tab.Entries = append(msg.Entries, tab.Entries...)
					// Adjust cursor position to maintain visual position
					tab.Cursor = oldCursor + len(msg.Entries)
					tab.LoadingMore = false
					log.Printf("[DEBUG] TUI LogEntryMsg: prepended paginated entries, tabID=%s, newEntries=%d, totalEntries=%d, cursorAdjusted=%d->%d",
						tab.ID, len(msg.Entries), len(tab.Entries), oldCursor, tab.Cursor)
				} else {
					// Append new entries to the end (newer logs or initial load)
					tab.Entries = append(tab.Entries, msg.Entries...)
					tab.Loading = false
					log.Printf("[DEBUG] TUI LogEntryMsg: appended entries, tabID=%s, totalEntries=%d", tab.ID, len(tab.Entries))
				}
				tab.Result = msg.Result
				tab.Template = msg.Template

				// Store pagination info
				tab.PaginationInfo = msg.PaginationInfo
				if tab.PaginationInfo != nil {
					log.Printf("[DEBUG] TUI LogEntryMsg: pagination info, hasMore=%v, nextToken=%s",
						tab.PaginationInfo.HasMore, tab.PaginationInfo.NextPageToken)
				}

				// Get available fields from message (for global fields view and autocomplete)
				if len(msg.Fields) > 0 {
					tab.Fields = msg.Fields
					log.Printf("[DEBUG] TUI LogEntryMsg: got fields, count=%d", len(tab.Fields))

					// Store field values in tab's search bar state
					tab.FieldValues = make(map[string][]string)
					for field, values := range tab.Fields {
						tab.FieldValues[field] = values
					}
				}

				// Extract available fields from entries and store in tab
				fieldSet := make(map[string]struct{})
				for _, entry := range tab.Entries {
					for field := range entry.Fields {
						fieldSet[field] = struct{}{}
					}
				}
				tab.AvailableFields = make([]string, 0, len(fieldSet))
				for field := range fieldSet {
					tab.AvailableFields = append(tab.AvailableFields, field)
				}
				sort.Strings(tab.AvailableFields)

				// Update available variables from search config and store in tab
				if msg.Result != nil {
					search := msg.Result.GetSearch()
					if search != nil && search.Variables != nil {
						tab.AvailableVariables = make([]string, 0, len(search.Variables))
						tab.VariableMetadata = make(map[string]string)
						for varName, varDef := range search.Variables {
							tab.AvailableVariables = append(tab.AvailableVariables, varName)
							tab.VariableMetadata[varName] = varDef.Description
						}
						sort.Strings(tab.AvailableVariables)
					}

					// NOTE: Auto-population of search bar from context config was removed.
					// It caused double-filtering: the server already filtered logs based on search params,
					// then the TUI applied those same filters client-side. Time drift/timezone differences
					// between client and server caused client-side filters to hide logs that the server
					// just returned, requiring users to press Escape to see logs.
					// The search bar should only contain filters explicitly added by the user or CLI args.
				}

				// If this is the active tab, update the global search bar
				if m.Tabs[m.ActiveTab].ID == tab.ID {
					m.SearchBar.FieldValues = tab.FieldValues
					m.SearchBar.AvailableFields = tab.AvailableFields
					m.SearchBar.AvailableVariables = tab.AvailableVariables
					m.SearchBar.VariableMetadata = tab.VariableMetadata
					// Also sync the search state
					m.SearchBar.State = tab.SearchState
					m.StatusBar.UpdateFromTab(tab)
					m.StatusBar.UpdateTimeRangeFromChips(m.SearchBar.State.Chips)
				}

				// Log pagination completion
				if msg.IsPagination {
					log.Printf("[DEBUG] TUI LogEntryMsg: pagination complete, loadingMore=%v", tab.LoadingMore)
				}

				// Start streaming subscription if channel is present
				if msg.StreamChan != nil {
					tab.StreamChan = msg.StreamChan
					cmds = append(cmds, waitForStreamBatch(tab))
					log.Printf("[DEBUG] TUI LogEntryMsg: started streaming subscription for tabID=%s", tab.ID)
				}

				// Start error monitoring if channel is present
				if msg.ErrorChan != nil {
					tab.ErrorChan = msg.ErrorChan
					cmds = append(cmds, waitForError(tab))
					log.Printf("[DEBUG] TUI LogEntryMsg: started error monitoring for tabID=%s", tab.ID)
				}

				// Always update viewport sizes before content
				// Viewport has default dimensions (80x20) even before WindowSizeMsg
				m.updateViewportSizes()
				m.updateViewportContent()
				m.updateSidebarContent()
				found = true
				break
			}
		}
		if !found {
			log.Printf("[WARN] TUI LogEntryMsg: tab not found, tabID=%s", msg.TabID)
		}

	case ErrorMsg:
		for _, tab := range m.Tabs {
			if tab.ID == msg.TabID {
				tab.Error = msg.Err
				tab.Loading = false

				// Update viewport to show the error
				m.updateViewportContent()

				// Continue monitoring for more errors if channel is still open
				if tab.ErrorChan != nil {
					cmds = append(cmds, waitForError(tab))
				}
				break
			}
		}

	case LoadingMsg:
		for _, tab := range m.Tabs {
			if tab.ID == msg.TabID {
				tab.Loading = msg.Loading
				break
			}
		}

	case StreamBatchMsg:
		// Handle streamed log entries (live streaming)
		for _, tab := range m.Tabs {
			if tab.ID == msg.TabID {
				// Append new entries
				tab.Entries = append(tab.Entries, msg.Entries...)
				log.Printf("[DEBUG] TUI StreamBatchMsg: appended %d entries, total=%d", len(msg.Entries), len(tab.Entries))

				// Update display if this is the active tab
				if m.Tabs[m.ActiveTab].ID == tab.ID {
					m.updateViewportContent()
				}

				// Continue subscription (recursive command pattern)
				cmds = append(cmds, waitForStreamBatch(tab))
				break
			}
		}

	case ClearStatusMsg:
		m.StatusBar.ClearMessage()
		return m, nil

	case InitMsg:
		// Load initial contexts
		log.Printf("[DEBUG] TUI InitMsg received, initialContexts=%v", m.InitialContexts)

		var initCmds []tea.Cmd
		for _, ctxID := range m.InitialContexts {
			search := m.InitialSearch
			if search == nil {
				search = &client.LogSearch{}
			}
			// Create a copy for each tab
			tabSearch := *search
			initCmds = append(initCmds, m.addTabCmd(ctxID, &tabSearch))
		}

		// Switch to first tab initially
		if len(m.Tabs) > 0 {
			m.switchToTab(0)
		}

		log.Printf("[DEBUG] TUI InitMsg: created %d tabs", len(m.Tabs))
		return m, tea.Batch(initCmds...)

	case AddTabMsg:
		cmd := m.addTabCmd(msg.ContextID, msg.Search)
		return m, cmd
	}

	return m, tea.Batch(cmds...)
}

// handleKeyPress processes keyboard input
//
//nolint:gocyclo // Keyboard handler with many keybindings
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.Keys.Quit):
		m.cleanup()
		return m, tea.Quit

	case key.Matches(msg, m.Keys.Help):
		m.ShowHelp = !m.ShowHelp
		return m, nil

	case key.Matches(msg, m.Keys.Search):
		m.Focus = FocusSearch
		return m, m.SearchBar.Focus()

	case key.Matches(msg, m.Keys.NextTab):
		if len(m.Tabs) > 0 {
			newIndex := (m.ActiveTab + 1) % len(m.Tabs)
			m.switchToTab(newIndex)
		}
		return m, nil

	case key.Matches(msg, m.Keys.PrevTab):
		if len(m.Tabs) > 0 {
			newIndex := (m.ActiveTab - 1 + len(m.Tabs)) % len(m.Tabs)
			m.switchToTab(newIndex)
		}
		return m, nil

	case key.Matches(msg, m.Keys.NewTab):
		if len(m.AvailableContexts) > 0 {
			m.Focus = FocusContextSelect
			m.ContextCursor = 0
		}
		return m, nil

	case key.Matches(msg, m.Keys.CloseTab):
		if len(m.Tabs) <= 1 {
			m.Confirmation = ConfirmQuitApp
		} else {
			m.Confirmation = ConfirmCloseTab
		}
		m.Focus = FocusConfirmation
		return m, nil

	case key.Matches(msg, m.Keys.ToggleSidebar):
		m.DetailsVisible = !m.DetailsVisible
		m.updateViewportSizes()
		m.updateSidebarContent()
		return m, nil

	case key.Matches(msg, m.Keys.ExpandSidebar):
		if m.DetailsVisible && m.SplitRatio > 0.3 {
			m.SplitRatio -= 0.05
			m.updateViewportSizes()
		}
		return m, nil

	case key.Matches(msg, m.Keys.ShrinkSidebar):
		if m.DetailsVisible && m.SplitRatio < 0.9 {
			m.SplitRatio += 0.05
			m.updateViewportSizes()
		}
		return m, nil

	case key.Matches(msg, m.Keys.Up):
		return m.moveCursor(-1)

	case key.Matches(msg, m.Keys.Down):
		return m.moveCursor(1)

	case key.Matches(msg, m.Keys.PageUp):
		return m.moveCursor(-10)

	case key.Matches(msg, m.Keys.PageDown):
		return m.moveCursor(10)

	case key.Matches(msg, m.Keys.Home):
		tab := m.CurrentTab()
		if tab == nil {
			return m, nil
		}

		// If already at top and more data available, trigger pagination
		if tab.Cursor == 0 &&
			tab.PaginationInfo != nil &&
			tab.PaginationInfo.HasMore &&
			!tab.LoadingMore {
			log.Printf("[DEBUG] TUI Home key: already at top, triggering pagination")
			tab.LoadingMore = true
			m.StatusBar.UpdateFromTab(tab)
			return m, m.loadMoreLogsCmd(tab)
		}

		return m.moveCursor(-len(tab.Entries))

	case key.Matches(msg, m.Keys.End):
		tab := m.CurrentTab()
		if tab != nil {
			return m.moveCursor(len(tab.Entries))
		}
		return m, nil

	case key.Matches(msg, m.Keys.Refresh):
		cmd := m.refreshCurrentTab()
		m.StatusBar.UpdateFromTab(m.CurrentTab())
		return m, cmd

	case key.Matches(msg, m.Keys.ClearSearch):
		m.SearchBar.Clear()
		m.updateViewportContent()
		return m, nil

	case key.Matches(msg, m.Keys.Copy):
		return m, m.copyJSONToClipboard()

	case key.Matches(msg, m.Keys.ToggleWrap):
		m.LineWrapping = !m.LineWrapping

		// Reset ViewOffset when changing wrap mode to avoid invalid state
		tab := m.CurrentTab()
		if tab != nil {
			tab.ViewOffset = 0
			// Ensure cursor is valid
			if tab.Cursor < 0 {
				tab.Cursor = 0
			}
			if len(tab.Entries) > 0 && tab.Cursor >= len(tab.Entries) {
				tab.Cursor = len(tab.Entries) - 1
			}
		}

		m.updateViewportContent()
		statusMsg := "Wrap: OFF"
		if m.LineWrapping {
			statusMsg = "Wrap: ON"
		}
		return m, m.showStatusMessage(statusMsg)
	}

	// Handle F key for sidebar mode toggle (not captured by Keys)
	if msg.String() == "F" && m.DetailsVisible {
		// Cycle through modes: Entry → JSON → Fields → Entry
		switch m.SidebarMode {
		case SidebarModeEntry:
			m.SidebarMode = SidebarModeJSON
		case SidebarModeJSON:
			m.SidebarMode = SidebarModeFields
		case SidebarModeFields:
			m.SidebarMode = SidebarModeEntry
		}
		m.updateSidebarContent()
		return m, nil
	}

	// Handle I key for inherit selection
	if msg.String() == "I" && len(m.AvailableSearches) > 0 {
		m.Focus = FocusInheritSelect
		m.InheritCursor = 0
		return m, nil
	}

	return m, nil
}

// handleSearchInput handles input when in search mode
func (m Model) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle escape to exit search mode (unless autocomplete is open)
	if msg.Type == tea.KeyEscape && !m.SearchBar.State.AutocompleteOpen {
		m.Focus = FocusList
		m.SearchBar.Blur()
		return m, nil
	}

	// Handle enter to commit and trigger a new search request
	if msg.Type == tea.KeyEnter && !m.SearchBar.State.AutocompleteOpen {
		log.Printf("[DEBUG] handleSearchInput: Enter pressed, triggering refresh")
		// Commit any pending input
		if m.SearchBar.State.CurrentInput != "" {
			m.SearchBar.commitCurrentInput()
		}
		m.Focus = FocusList
		m.SearchBar.Blur()
		// Save search bar state to current tab
		m.saveSearchBarToTab(m.CurrentTab())
		// Update status bar with time range from chips
		m.StatusBar.UpdateTimeRangeFromChips(m.SearchBar.State.Chips)
		// Trigger a new search request with the updated chips (time range, inherits, etc.)
		cmd := m.refreshCurrentTab()
		// Update status bar to show loading state
		m.StatusBar.UpdateFromTab(m.CurrentTab())
		return m, cmd
	}

	// Delegate to search bar
	var cmd tea.Cmd
	m.SearchBar, cmd = m.SearchBar.Update(msg)
	// Don't update viewport content while typing - wait for Enter to apply changes
	// Only update status bar with time range from chips (preview what will be applied)
	m.StatusBar.UpdateTimeRangeFromChips(m.SearchBar.State.Chips)
	return m, cmd
}

// handleContextSelect handles input when selecting a context for new tab
func (m Model) handleContextSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.Focus = FocusList
		return m, nil

	case tea.KeyEnter:
		if m.ContextCursor < len(m.AvailableContexts) {
			selectedContext := m.AvailableContexts[m.ContextCursor]
			// Save current tab's search bar state before creating new tab
			m.saveSearchBarToTab(m.CurrentTab())
			m.Focus = FocusList
			return m, m.AddTab(selectedContext, &client.LogSearch{})
		}
		return m, nil

	case tea.KeyUp:
		if m.ContextCursor > 0 {
			m.ContextCursor--
		}
		return m, nil

	case tea.KeyDown:
		if m.ContextCursor < len(m.AvailableContexts)-1 {
			m.ContextCursor++
		}
		return m, nil
	}

	// Handle j/k for navigation
	switch msg.String() {
	case "j":
		if m.ContextCursor < len(m.AvailableContexts)-1 {
			m.ContextCursor++
		}
	case "k":
		if m.ContextCursor > 0 {
			m.ContextCursor--
		}
	}

	return m, nil
}

// handleInheritSelect handles input when selecting inherited searches
func (m Model) handleInheritSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.Focus = FocusList
		return m, nil

	case tea.KeyEnter:
		// Confirm and close, trigger refresh with loading indicator
		m.Focus = FocusList
		cmd := m.refreshCurrentTab()
		m.StatusBar.UpdateFromTab(m.CurrentTab())
		return m, cmd

	case tea.KeySpace:
		// Toggle current search template
		if m.InheritCursor < len(m.AvailableSearches) {
			search := m.AvailableSearches[m.InheritCursor]
			m.ActiveSearches[search] = !m.ActiveSearches[search]
		}
		return m, nil

	case tea.KeyUp:
		if m.InheritCursor > 0 {
			m.InheritCursor--
		}
		return m, nil

	case tea.KeyDown:
		if m.InheritCursor < len(m.AvailableSearches)-1 {
			m.InheritCursor++
		}
		return m, nil
	}

	// Handle j/k for navigation, space for toggle
	switch msg.String() {
	case "j":
		if m.InheritCursor < len(m.AvailableSearches)-1 {
			m.InheritCursor++
		}
	case "k":
		if m.InheritCursor > 0 {
			m.InheritCursor--
		}
	case " ":
		// Toggle current search template
		if m.InheritCursor < len(m.AvailableSearches) {
			search := m.AvailableSearches[m.InheritCursor]
			m.ActiveSearches[search] = !m.ActiveSearches[search]
		}
	}

	return m, nil
}

// handleConfirmation handles input when in confirmation mode
func (m Model) handleConfirmation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.Focus = FocusList
		if m.Confirmation == ConfirmQuitApp {
			m.cleanup()
			return m, tea.Quit
		}
		return m, m.closeCurrentTab()

	case "n", "N":
		m.Focus = FocusList
		return m, nil
	}

	if msg.Type == tea.KeyEscape {
		m.Focus = FocusList
		return m, nil
	}

	return m, nil
}

// moveCursor moves the cursor by delta positions
func (m Model) moveCursor(delta int) (Model, tea.Cmd) {
	tab := m.CurrentTab()
	if tab == nil || len(tab.Entries) == 0 {
		return m, nil
	}

	oldCursor := tab.Cursor
	newCursor := tab.Cursor + delta
	if newCursor < 0 {
		newCursor = 0
	}
	if newCursor >= len(tab.Entries) {
		newCursor = len(tab.Entries) - 1
	}

	// Trigger pagination if trying to move up past the top
	if delta < 0 && tab.Cursor == 0 &&
		tab.PaginationInfo != nil &&
		tab.PaginationInfo.HasMore &&
		!tab.LoadingMore {
		log.Printf("[DEBUG] TUI moveCursor: triggering pagination from top boundary")
		tab.LoadingMore = true
		m.StatusBar.UpdateFromTab(tab)
		return m, m.loadMoreLogsCmd(tab)
	}

	// Only update if cursor actually changed
	if newCursor == oldCursor {
		return m, nil
	}

	tab.Cursor = newCursor
	m.updateViewportContent()
	m.updateSidebarContent()

	// Check if we need to fetch more data (pagination)
	// Trigger when scrolling near the top and there's more data available
	const paginationThreshold = 5 // Trigger when within 5 entries of the top
	if newCursor < paginationThreshold &&
		delta < 0 && // Only trigger when moving UP
		tab.PaginationInfo != nil &&
		tab.PaginationInfo.HasMore &&
		!tab.LoadingMore {
		log.Printf("[DEBUG] TUI moveCursor: triggering pagination, cursor=%d, threshold=%d, delta=%d", newCursor, paginationThreshold, delta)
		tab.LoadingMore = true
		m.StatusBar.UpdateFromTab(tab) // Update status bar to show loading indicator
		return m, m.loadMoreLogsCmd(tab)
	}

	return m, nil
}

// closeCurrentTab closes the active tab
func (m *Model) closeCurrentTab() tea.Cmd {
	if len(m.Tabs) == 0 {
		return nil
	}

	tab := m.CurrentTab()
	if tab != nil && tab.CancelFunc != nil {
		tab.CancelFunc()
	}

	m.Tabs = append(m.Tabs[:m.ActiveTab], m.Tabs[m.ActiveTab+1:]...)
	if m.ActiveTab >= len(m.Tabs) && m.ActiveTab > 0 {
		m.ActiveTab--
	}

	if len(m.Tabs) == 0 {
		return tea.Quit
	}

	m.updateViewportContent()
	return nil
}

// refreshCurrentTab reloads the current tab's logs
func (m *Model) refreshCurrentTab() tea.Cmd {
	log.Printf("[DEBUG] refreshCurrentTab: called, chips count=%d", len(m.SearchBar.State.Chips))
	for i, c := range m.SearchBar.State.Chips {
		log.Printf("[DEBUG] refreshCurrentTab: chip[%d] type=%d field=%s op=%s value=%s", i, c.Type, c.Field, c.Operator, c.Value)
	}

	tab := m.CurrentTab()
	if tab == nil {
		return nil
	}

	if tab.CancelFunc != nil {
		tab.CancelFunc()
	}

	tab.Entries = make([]client.LogEntry, 0)
	tab.Cursor = 0
	tab.Loading = true
	tab.Error = nil

	// Clear JSON cache since entries will be reloaded
	tab.JSONCache = nil

	// Rebuild search entirely from chips (time range, fields, etc.)
	// This ensures removed chips are not included in the search
	chipSearch := m.SearchBar.BuildSearchFromChips()

	// Get variable assignments from chips
	_, vars := m.SearchBar.BuildSearchModifiers()

	// Apply variable assignments to runtime vars
	for k, v := range vars {
		m.RuntimeVars[k] = v
	}

	// Replace tab search with chip-based search
	// Only preserve settings that don't affect filtering
	if tab.Search != nil {
		// Keep display/extraction settings from original (not filtering)
		chipSearch.PrinterOptions = tab.Search.PrinterOptions
		chipSearch.FieldExtraction = tab.Search.FieldExtraction
		chipSearch.Size = tab.Search.Size
		chipSearch.Variables = tab.Search.Variables
		// Note: Don't preserve NativeQuery or Options as they may contain old filters
	}
	tab.Search = chipSearch

	log.Printf("[DEBUG] refreshCurrentTab: chipSearch.Fields=%v, chipSearch.Range=%+v", chipSearch.Fields, chipSearch.Range)

	// Extract inherits from ChipTypeInherit chips
	var inherits []string
	for _, chip := range m.SearchBar.State.Chips {
		if chip.Type == ChipTypeInherit {
			inherits = append(inherits, chip.Value)
		}
	}

	// Also include inherits from ActiveSearches (from I key menu)
	for search, active := range m.ActiveSearches {
		if active {
			// Check if not already in inherits list
			found := false
			for _, existing := range inherits {
				if existing == search {
					found = true
					break
				}
			}
			if !found {
				inherits = append(inherits, search)
			}
		}
	}

	// Sort for consistent ordering
	sort.Strings(inherits)
	tab.Inherits = inherits

	log.Printf("[DEBUG] refreshCurrentTab: extracted inherits=%v from chips", inherits)
	return m.loadTabLogsCmd(tab)
}

// copyJSONToClipboard copies JSON from the selected entry to the system clipboard
func (m *Model) copyJSONToClipboard() tea.Cmd {
	tab := m.CurrentTab()
	if tab == nil || len(tab.Entries) == 0 {
		return m.showStatusMessage("No entry selected")
	}

	if tab.Cursor >= len(tab.Entries) {
		return m.showStatusMessage("Invalid cursor position")
	}

	entry := tab.Entries[tab.Cursor]

	// Detect JSON in message
	jsonStrings, found := m.detectAndCacheJSON(tab, entry.Message)
	if !found || len(jsonStrings) == 0 {
		return m.showStatusMessage("No JSON found in this entry")
	}

	// Format JSON for clipboard (all detected JSON objects)
	var clipboardContent strings.Builder
	for i, jsonStr := range jsonStrings {
		// Parse and re-format for clean output
		var obj interface{}
		if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
			continue
		}

		// Pretty-print JSON
		formatted, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			continue
		}

		if i > 0 {
			clipboardContent.WriteString("\n\n")
		}
		clipboardContent.Write(formatted)
	}

	if clipboardContent.Len() == 0 {
		return m.showStatusMessage("Failed to format JSON")
	}

	// Copy to clipboard
	if err := clipboard.WriteAll(clipboardContent.String()); err != nil {
		return m.showStatusMessage(fmt.Sprintf("Clipboard error: %v", err))
	}

	// Show success message
	count := len(jsonStrings)
	if count == 1 {
		return m.showStatusMessage("JSON copied to clipboard")
	}
	return m.showStatusMessage(fmt.Sprintf("%d JSON objects copied to clipboard", count))
}

// showStatusMessage temporarily shows a message in the status bar
// Returns a command that will clear the message after a delay
func (m *Model) showStatusMessage(message string) tea.Cmd {
	m.StatusBar.SetMessage(message)
	return tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
		return ClearStatusMsg{}
	})
}

// cleanup cancels all active goroutines
func (m *Model) cleanup() {
	for _, tab := range m.Tabs {
		if tab.CancelFunc != nil {
			tab.CancelFunc()
		}
	}
}

// updateViewportSizes recalculates component sizes
func (m *Model) updateViewportSizes() {
	headerHeight := 2 // Tab bar
	statusHeight := 4 // Status bar (2 lines + borders)
	footerHeight := 3 // Search bar + help (may grow with autocomplete)
	mainHeight := m.Height - headerHeight - statusHeight - footerHeight

	if mainHeight < 1 {
		mainHeight = 1
	}

	// Update status bar width
	m.StatusBar.Width = m.Width
	m.SearchBar.Width = m.Width

	if m.DetailsVisible {
		listWidth := int(float64(m.Width) * m.SplitRatio)
		sidebarWidth := m.Width - listWidth - 1 // -1 for border

		m.Viewport.Width = listWidth
		m.Viewport.Height = mainHeight

		m.SidebarVP.Width = sidebarWidth
		m.SidebarVP.Height = mainHeight
	} else {
		m.Viewport.Width = m.Width
		m.Viewport.Height = mainHeight
	}
}

// updateViewportContent refreshes the log list content
//
//nolint:gocyclo // Complex viewport rendering with multiple display modes
func (m *Model) updateViewportContent() {
	tab := m.CurrentTab()
	if tab == nil {
		m.Viewport.SetContent("No tabs open. Press Ctrl+T to create a new tab.")
		return
	}

	if tab.Loading {
		m.Viewport.SetContent("Loading...")
		return
	}

	if tab.Error != nil {
		// Show error with styling and helpful information
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")). // Red
			Bold(true).
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9"))

		errorContent := fmt.Sprintf("❌ Backend Error\n\n%v\n\nPress 'r' to retry or 'q' to quit", tab.Error)
		m.Viewport.SetContent(errorStyle.Render(errorContent))
		return
	}

	if len(tab.Entries) == 0 {
		m.Viewport.SetContent("No log entries found.")
		return
	}

	// Filter entries using SearchBar (chips + free text)
	entries := tab.Entries
	filter := m.SearchBar.BuildFilter()
	if filter != nil {
		filtered := make([]client.LogEntry, 0)
		for _, entry := range entries {
			if filter.Match(entry) {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	} else {
		// Fallback to simple text search for current input
		searchTerm := strings.ToLower(m.SearchBar.GetFreeTextSearch())
		if searchTerm != "" {
			filtered := make([]client.LogEntry, 0)
			for _, entry := range entries {
				if strings.Contains(strings.ToLower(entry.Message), searchTerm) ||
					strings.Contains(strings.ToLower(entry.Level), searchTerm) {
					filtered = append(filtered, entry)
				}
			}
			entries = filtered
		}
	}

	// Update status bar with filtered count
	m.StatusBar.SetFilteredCount(len(entries))

	// Ensure cursor is within bounds of filtered entries
	if len(entries) == 0 {
		tab.Cursor = 0
		tab.ViewOffset = 0
		m.Viewport.SetContent("No log entries found (after filtering).")
		return
	}

	if tab.Cursor >= len(entries) {
		tab.Cursor = len(entries) - 1
	}
	if tab.Cursor < 0 {
		tab.Cursor = 0
	}

	// Ensure ViewOffset is within bounds
	if tab.ViewOffset < 0 {
		tab.ViewOffset = 0
	}
	if tab.ViewOffset >= len(entries) {
		tab.ViewOffset = len(entries) - 1
		if tab.ViewOffset < 0 {
			tab.ViewOffset = 0
		}
	}

	// Calculate visible window
	visibleLines := m.Viewport.Height
	if visibleLines < 1 {
		visibleLines = 1
	}

	// Build content - handle multiline rendering differently based on wrap mode
	if m.LineWrapping {
		// Wrap mode: more complex scrolling due to variable entry heights
		// Strategy: Ensure cursor entry is visible, then render around it

		// Calculate height of each entry from ViewOffset to cursor
		cursorVisible := false
		totalVisualLines := 0

		// First pass: check if cursor is visible with current ViewOffset
		for i := tab.ViewOffset; i < len(entries) && totalVisualLines < visibleLines; i++ {
			if i < 0 || i >= len(entries) {
				break // Safety: stop if index is invalid
			}

			entry := entries[i]
			isSelected := i == tab.Cursor
			rendered := m.renderLogEntry(entry, isSelected, m.Viewport.Width, tab)

			entryHeight := countVisualLines(rendered, m.Viewport.Width)
			if entryHeight < 1 {
				entryHeight = 1 // Minimum 1 line per entry
			}

			if i == tab.Cursor {
				// Check if cursor entry fits in remaining space
				if totalVisualLines+entryHeight <= visibleLines {
					cursorVisible = true
				}
				break
			}
			totalVisualLines += entryHeight
		}

		// Adjust ViewOffset if cursor is not visible
		if !cursorVisible || tab.Cursor < tab.ViewOffset {
			// Cursor moved: recalculate ViewOffset to make it visible
			if tab.Cursor < tab.ViewOffset {
				// Scrolled up: put cursor at top
				tab.ViewOffset = tab.Cursor
			} else {
				// Scrolled down: put cursor at bottom, work backwards
				tab.ViewOffset = tab.Cursor
				accumulatedHeight := 0

				// Work backwards from cursor to find good ViewOffset
				for i := tab.Cursor; i >= 0; i-- {
					if i < 0 || i >= len(entries) {
						break // Safety: stop if index is invalid
					}

					entry := entries[i]
					isSelected := i == tab.Cursor
					rendered := m.renderLogEntry(entry, isSelected, m.Viewport.Width, tab)
					entryHeight := countVisualLines(rendered, m.Viewport.Width)
					if entryHeight < 1 {
						entryHeight = 1 // Minimum 1 line per entry
					}

					if accumulatedHeight+entryHeight > visibleLines && i < tab.Cursor {
						// This entry won't fit, start from next one
						tab.ViewOffset = i + 1
						break
					}

					accumulatedHeight += entryHeight
					tab.ViewOffset = i

					if accumulatedHeight >= visibleLines {
						break
					}
				}
			}
		}

		// Clamp ViewOffset with extra safety
		if tab.ViewOffset < 0 {
			tab.ViewOffset = 0
		}
		if tab.ViewOffset >= len(entries) {
			tab.ViewOffset = len(entries) - 1
			if tab.ViewOffset < 0 {
				tab.ViewOffset = 0
			}
		}

		// Ensure cursor is also within bounds before rendering
		if tab.Cursor >= len(entries) {
			tab.Cursor = len(entries) - 1
		}
		if tab.Cursor < 0 {
			tab.Cursor = 0
		}

		// Second pass: render from ViewOffset
		var visualLines []string
		totalVisualLines = 0

		for i := tab.ViewOffset; i < len(entries) && totalVisualLines < visibleLines; i++ {
			if i < 0 || i >= len(entries) {
				continue // Skip invalid indices
			}

			entry := entries[i]
			isSelected := i == tab.Cursor
			rendered := m.renderLogEntry(entry, isSelected, m.Viewport.Width, tab)

			// Split rendered output into individual lines and wrap long lines
			entryLines := strings.Split(rendered, "\n")
			for _, entryLine := range entryLines {
				// Wrap long lines to viewport width
				wrapped := wrapLine(entryLine, m.Viewport.Width)
				for _, wrappedLine := range wrapped {
					if totalVisualLines < visibleLines {
						visualLines = append(visualLines, wrappedLine)
						totalVisualLines++
					}
				}
			}
		}

		// Pad with empty lines if needed
		for totalVisualLines < visibleLines {
			visualLines = append(visualLines, "")
			totalVisualLines++
		}

		// Prepend loading indicator if pagination is loading
		if tab.LoadingMore {
			loadingLine := m.Styles.SidebarKey.Foreground(ColorPrimary).Render("⏳ Loading more logs...")
			visualLines = append([]string{loadingLine}, visualLines...)
		}

		m.Viewport.SetContent(strings.Join(visualLines, "\n"))
	} else {
		// No-wrap mode: one entry = one line (original behavior)
		// Adjust ViewOffset to keep cursor visible
		if tab.Cursor < tab.ViewOffset {
			tab.ViewOffset = tab.Cursor
		} else if tab.Cursor >= tab.ViewOffset+visibleLines {
			tab.ViewOffset = tab.Cursor - visibleLines + 1
		}

		// Clamp ViewOffset again after adjustment
		maxOffset := len(entries) - visibleLines
		if maxOffset < 0 {
			maxOffset = 0
		}
		if tab.ViewOffset > maxOffset {
			tab.ViewOffset = maxOffset
		}
		if tab.ViewOffset < 0 {
			tab.ViewOffset = 0
		}

		var lines []string
		endIdx := tab.ViewOffset + visibleLines
		if endIdx > len(entries) {
			endIdx = len(entries)
		}

		// Safety check: ensure ViewOffset is valid
		if tab.ViewOffset >= len(entries) {
			tab.ViewOffset = 0
		}

		for i := tab.ViewOffset; i < endIdx; i++ {
			if i < 0 || i >= len(entries) {
				continue // Skip invalid indices
			}
			entry := entries[i]
			isSelected := i == tab.Cursor
			line := m.renderLogEntry(entry, isSelected, m.Viewport.Width, tab)
			lines = append(lines, line)
		}

		// Pad with empty lines if needed
		for len(lines) < visibleLines {
			lines = append(lines, "")
		}

		// Prepend loading indicator if pagination is loading
		if tab.LoadingMore {
			loadingLine := m.Styles.SidebarKey.Foreground(ColorPrimary).Render("⏳ Loading more logs...")
			lines = append([]string{loadingLine}, lines...)
		}

		m.Viewport.SetContent(strings.Join(lines, "\n"))
	}
}

// updateSidebarContent refreshes the sidebar content
func (m *Model) updateSidebarContent() {
	if !m.DetailsVisible {
		return
	}

	tab := m.CurrentTab()
	if tab == nil {
		m.SidebarVP.SetContent("No tab selected")
		return
	}

	// Render based on sidebar mode
	switch m.SidebarMode {
	case SidebarModeFields:
		m.SidebarVP.SetContent(m.renderGlobalFields())
		return
	case SidebarModeJSON:
		if len(tab.Entries) == 0 || tab.Cursor >= len(tab.Entries) {
			m.SidebarVP.SetContent("No entry selected")
			return
		}
		entry := tab.Entries[tab.Cursor]
		m.SidebarVP.SetContent(m.renderEntryJSON(entry))
		return
	case SidebarModeEntry:
		// Entry details mode (default)
		if len(tab.Entries) == 0 {
			m.SidebarVP.SetContent("No entry selected")
			return
		}
		if tab.Cursor >= len(tab.Entries) {
			return
		}
		entry := tab.Entries[tab.Cursor]
		m.SidebarVP.SetContent(m.renderEntryDetails(entry))
		return
	}
}

// renderLogEntry renders a single log entry line using the tab's printer template
func (m *Model) renderLogEntry(entry client.LogEntry, selected bool, maxWidth int, tab *Tab) string {
	if maxWidth < 20 {
		maxWidth = 20
	}

	var line string
	var hasJSON bool
	var jsonSummary string

	// Use the tab's template if available
	if tab != nil && tab.Template != nil {
		var buf bytes.Buffer
		if err := tab.Template.Execute(&buf, entry); err != nil {
			// Fallback to format with message on template error
			line = fmt.Sprintf("[%s] %s %s", entry.Timestamp.Format("15:04:05"), entry.Level, entry.Message)
		} else {
			line = buf.String()
		}
	} else {
		// Default format with message if no template
		line = fmt.Sprintf("[%s] [%s] %s %s", entry.Timestamp.Format("15:04:05"), entry.ContextID, entry.Level, entry.Message)
	}

	// Detect JSON in the message (check cache or detect)
	jsonStrings, found := m.detectAndCacheJSON(tab, entry.Message)
	if found {
		hasJSON = true
		// Create summary for first JSON object/array
		count, isObject := countJSONKeys(jsonStrings[0])
		if isObject {
			jsonSummary = fmt.Sprintf("[JSON: %d keys]", count)
		} else {
			jsonSummary = fmt.Sprintf("[JSON Array: %d items]", count)
		}

		if len(jsonStrings) > 1 {
			jsonSummary += fmt.Sprintf(" +%d more", len(jsonStrings)-1)
		}
	}

	// Handle wrapping mode
	if m.LineWrapping {
		// Wrap mode: Keep newlines, don't truncate
		// Clean up carriage returns and tabs but keep newlines
		line = strings.ReplaceAll(line, "\r", "")
		line = strings.ReplaceAll(line, "\t", "  ") // Convert tabs to 2 spaces

		// Add JSON indicator if present and not already shown
		if hasJSON && jsonSummary != "" && !strings.Contains(line, "[JSON") {
			indicator := m.Styles.SidebarKey.Render(jsonSummary)
			line = line + " " + indicator
		}

		// Apply selection or normal style (no width constraint for wrapping)
		if selected {
			return m.Styles.LogSelected.Render(line)
		}
		return m.Styles.LogEntry.Render(line)
	}

	// No-wrap mode (default): Single line, truncate if needed
	// Replace multi-line ExpandJson output with compact summary
	// Look for patterns like "\n{" or "\n[" which indicate expanded JSON
	if strings.Contains(line, "\n{") || strings.Contains(line, "\n[") {
		// Find where JSON expansion starts
		if idx := strings.Index(line, "\n"); idx != -1 {
			// Keep everything before the newline, replace JSON with summary
			line = line[:idx]
			if jsonSummary != "" {
				line += " " + jsonSummary
			}
		}
	}

	// Clean up the line: replace newlines and tabs with spaces
	line = strings.ReplaceAll(line, "\n", " ")
	line = strings.ReplaceAll(line, "\r", "")
	line = strings.ReplaceAll(line, "\t", " ")

	// Add JSON indicator at the end if present and not already shown
	if hasJSON && jsonSummary != "" && !strings.Contains(line, "[JSON") {
		// Reserve space for indicator
		indicatorSpace := len(jsonSummary) + 1
		maxLineWidth := maxWidth - indicatorSpace

		// Truncate main line if needed
		lineRunes := []rune(line)
		if len(lineRunes) > maxLineWidth {
			if maxLineWidth > 3 {
				line = string(lineRunes[:maxLineWidth-3]) + "..."
			} else {
				line = string(lineRunes[:maxLineWidth])
			}
		}

		// Append JSON indicator with styling
		indicator := m.Styles.SidebarKey.Render(jsonSummary)
		line = line + " " + indicator
	} else {
		// Truncate line to fit maxWidth (before styling to preserve ANSI codes)
		lineRunes := []rune(line)
		if len(lineRunes) > maxWidth {
			if maxWidth > 3 {
				line = string(lineRunes[:maxWidth-3]) + "..."
			} else {
				line = string(lineRunes[:maxWidth])
			}
		}
	}

	// Apply selection or normal style
	if selected {
		return m.Styles.LogSelected.Width(maxWidth).Render(line)
	}
	return m.Styles.LogEntry.Width(maxWidth).Render(line)
}

// countVisualLines counts how many visual lines an entry will take when rendered
// This accounts for newlines in the template and wrapping of long lines
func countVisualLines(rendered string, maxWidth int) int {
	if maxWidth < 1 {
		maxWidth = 1
	}

	count := 0
	lines := strings.Split(rendered, "\n")
	for _, line := range lines {
		wrapped := wrapLine(line, maxWidth)
		count += len(wrapped)
	}
	return count
}

// wrapLine wraps a long line to fit within maxWidth, preserving ANSI codes
// Returns a slice of wrapped lines
func wrapLine(line string, maxWidth int) []string {
	if maxWidth < 1 {
		maxWidth = 1
	}

	// Quick check: if line fits, return as-is
	plainLen := lipgloss.Width(line) // lipgloss.Width handles ANSI codes
	if plainLen <= maxWidth {
		return []string{line}
	}

	// For lines with ANSI codes, we need to be more careful
	// Simple approach: split into runes and track visible width
	runes := []rune(line)
	var result []string
	var currentLine strings.Builder
	visibleWidth := 0
	inEscape := false

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// Track ANSI escape sequences
		if r == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			inEscape = true
			currentLine.WriteRune(r)
			continue
		}

		if inEscape {
			currentLine.WriteRune(r)
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}

		// Regular visible character
		currentLine.WriteRune(r)
		visibleWidth++

		// Check if we need to wrap
		if visibleWidth >= maxWidth {
			result = append(result, currentLine.String())
			currentLine.Reset()
			visibleWidth = 0
		}
	}

	// Add remaining content
	if currentLine.Len() > 0 {
		result = append(result, currentLine.String())
	}

	if len(result) == 0 {
		return []string{""}
	}

	return result
}

// detectAndCacheJSON detects JSON in a log entry and caches the result.
// Returns the cached JSON strings and whether JSON was found.
func (m *Model) detectAndCacheJSON(tab *Tab, message string) ([]string, bool) {
	if tab.JSONCache == nil {
		tab.JSONCache = make(map[string][]string)
	}

	// Check cache first
	if cached, ok := tab.JSONCache[message]; ok {
		return cached, len(cached) > 0
	}

	// Use existing FindJSON function from printer package
	jsonStrings := printer.FindJSON(message)

	// Cache the result (even if empty, to avoid re-scanning)
	tab.JSONCache[message] = jsonStrings

	return jsonStrings, len(jsonStrings) > 0
}

// countJSONKeys counts the number of keys/items in a JSON string.
// Returns (count, isObject). isObject is true for objects, false for arrays.
func countJSONKeys(jsonStr string) (int, bool) {
	var obj interface{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return 0, false
	}

	switch v := obj.(type) {
	case map[string]interface{}:
		return len(v), true // Object with N keys
	case []interface{}:
		return len(v), false // Array with N items
	default:
		return 0, false
	}
}

// renderEntryDetails renders the sidebar content for an entry
func (m *Model) renderEntryDetails(entry client.LogEntry) string {
	var b strings.Builder

	// Title
	b.WriteString(m.Styles.SidebarTitle.Render("Entry Details"))
	b.WriteString("\n\n")

	// Core fields
	writeField := func(key, value string) {
		b.WriteString(m.Styles.SidebarKey.Render(key + ": "))
		b.WriteString(m.Styles.SidebarValue.Render(value))
		b.WriteString("\n")
	}

	writeField("Timestamp", entry.Timestamp.Format(time.RFC3339))
	writeField("Level", entry.Level)
	if entry.ContextID != "" {
		writeField("Context", entry.ContextID)
	}

	// Fields (sorted alphabetically)
	if len(entry.Fields) > 0 {
		b.WriteString("\n")
		b.WriteString(m.Styles.SidebarTitle.Render("Fields"))
		b.WriteString("\n")

		// Sort field keys alphabetically
		fieldKeys := make([]string, 0, len(entry.Fields))
		for key := range entry.Fields {
			fieldKeys = append(fieldKeys, key)
		}
		sort.Strings(fieldKeys)

		// Render fields in sorted order
		for _, key := range fieldKeys {
			val := entry.Fields[key]
			valStr := fmt.Sprintf("%v", val)
			// Check if it's a nested object
			if nested, ok := val.(map[string]interface{}); ok {
				jsonBytes, err := json.MarshalIndent(nested, "  ", "  ")
				if err == nil {
					valStr = string(jsonBytes)
				}
			}
			b.WriteString(m.Styles.SidebarKey.Render(key + ":"))
			b.WriteString("\n  ")
			b.WriteString(m.Styles.SidebarValue.Render(valStr))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// renderGlobalFields renders the sidebar content for global fields view
func (m *Model) renderGlobalFields() string {
	tab := m.CurrentTab()
	if tab == nil || len(tab.Fields) == 0 {
		return m.Styles.SidebarValue.Render("No fields available.\nFields are populated from the\nlog source's field discovery.")
	}

	var b strings.Builder

	// Title
	b.WriteString(m.Styles.SidebarTitle.Render("Global Fields"))
	b.WriteString("\n\n")

	// Sort field names for consistent display
	fieldNames := make([]string, 0, len(tab.Fields))
	for field := range tab.Fields {
		fieldNames = append(fieldNames, field)
	}
	sort.Strings(fieldNames)

	// Render each field with its top values
	maxValues := 5 // Show top 5 values per field
	for _, field := range fieldNames {
		values := tab.Fields[field]

		// Field name header
		b.WriteString(m.Styles.SidebarKey.Render(field + ":"))
		b.WriteString("\n")

		// Show top N values
		displayCount := len(values)
		if displayCount > maxValues {
			displayCount = maxValues
		}

		for i := 0; i < displayCount; i++ {
			b.WriteString("  ")
			b.WriteString(m.Styles.SidebarValue.Render("• " + values[i]))
			b.WriteString("\n")
		}

		// Show "more" indicator if there are additional values
		if len(values) > maxValues {
			remaining := len(values) - maxValues
			b.WriteString("  ")
			b.WriteString(m.Styles.SidebarValue.Foreground(ColorMuted).Render(
				fmt.Sprintf("... +%d more", remaining)))
			b.WriteString("\n")
		}

		b.WriteString("\n")
	}

	return b.String()
}

// renderEntryJSON renders formatted JSON from the selected log entry
func (m *Model) renderEntryJSON(entry client.LogEntry) string {
	tab := m.CurrentTab()
	if tab == nil {
		return m.Styles.SidebarValue.Render("No tab selected")
	}

	var b strings.Builder

	// Title
	b.WriteString(m.Styles.SidebarTitle.Render("JSON Content"))
	b.WriteString("\n\n")

	// Get cached JSON
	jsonStrings, found := m.detectAndCacheJSON(tab, entry.Message)
	if !found || len(jsonStrings) == 0 {
		b.WriteString(m.Styles.SidebarValue.Render("No JSON detected in this entry"))
		b.WriteString("\n\n")
		b.WriteString(m.Styles.SidebarKey.Render("Press 'F' to toggle sidebar views"))
		return b.String()
	}

	// Render each JSON object/array with syntax highlighting
	for i, jsonStr := range jsonStrings {
		if i > 0 {
			b.WriteString("\n\n")
			b.WriteString(m.Styles.SidebarKey.Render(fmt.Sprintf("--- JSON Object %d ---", i+1)))
			b.WriteString("\n")
		}

		// Parse JSON
		var obj interface{}
		if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
			b.WriteString(m.Styles.SidebarValue.Foreground(ColorError).Render(
				fmt.Sprintf("Error parsing JSON: %v", err)))
			continue
		}

		// Format with colorjson (respects color settings)
		if printer.IsColorEnabled() {
			f := colorjson.NewFormatter()
			f.Indent = 2
			formatted, err := f.Marshal(obj)
			if err != nil {
				b.WriteString(m.Styles.SidebarValue.Foreground(ColorError).Render(
					fmt.Sprintf("Error formatting JSON: %v", err)))
				continue
			}
			b.Write(formatted)
		} else {
			// Plain formatting without colors
			formatted, err := json.MarshalIndent(obj, "", "  ")
			if err != nil {
				b.WriteString(m.Styles.SidebarValue.Foreground(ColorError).Render(
					fmt.Sprintf("Error formatting JSON: %v", err)))
				continue
			}
			b.Write(formatted)
		}
	}

	b.WriteString("\n\n")
	b.WriteString(m.Styles.SidebarKey.Foreground(ColorMuted).Render("Hint: Press 'c' or 'y' to copy JSON"))

	return b.String()
}

// View renders the TUI
func (m Model) View() string {
	if m.Width == 0 || m.Height == 0 {
		return "Loading..."
	}

	// Render context selection overlay if active
	if m.Focus == FocusContextSelect {
		return m.renderContextSelectOverlay()
	}

	// Render inherit selection overlay if active
	if m.Focus == FocusInheritSelect {
		return m.renderInheritSelectOverlay()
	}

	// Render confirmation overlay if active
	if m.Focus == FocusConfirmation {
		return m.renderConfirmationOverlay()
	}

	sections := make([]string, 0, 4)

	// Header (tabs)
	sections = append(sections, m.renderTabs())

	// Main content area
	mainContent := m.renderMainArea()
	sections = append(sections, mainContent)

	// Status bar (between viewport and search)
	sections = append(sections, m.StatusBar.View())

	// Search bar and help
	sections = append(sections, m.renderSearchFooter())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderConfirmationOverlay renders the confirmation modal
func (m Model) renderConfirmationOverlay() string {
	var title, message string
	if m.Confirmation == ConfirmQuitApp {
		title = "Quit Application?"
		message = "Are you sure you want to quit? (y/N)"
	} else {
		title = "Close Tab?"
		message = "Are you sure you want to close this tab? (y/N)"
	}

	// Build the modal content
	content := lipgloss.JoinVertical(lipgloss.Center,
		m.Styles.SidebarTitle.Render(title),
		"",
		m.Styles.SidebarValue.Render(message),
	)

	// Box it
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 4).
		Align(lipgloss.Center)

	modal := modalStyle.Render(content)

	// Center on screen
	return lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		modal,
	)
}

// renderContextSelectOverlay renders the context selection modal
func (m Model) renderContextSelectOverlay() string {
	// Title
	title := m.Styles.SidebarTitle.Render("Select Context for New Tab")

	// Context list
	items := make([]string, 0, len(m.AvailableContexts))
	for i, ctx := range m.AvailableContexts {
		style := m.Styles.LogEntry
		if i == m.ContextCursor {
			style = m.Styles.LogSelected
		}

		// Add description if available
		desc := ""
		if m.Config != nil {
			if ctxCfg, ok := m.Config.Contexts[ctx]; ok && ctxCfg.Description != "" {
				desc = " - " + ctxCfg.Description
			}
		}

		items = append(items, style.Render(fmt.Sprintf("  %s%s", ctx, desc)))
	}

	list := strings.Join(items, "\n")

	// Help text
	help := m.Styles.HelpBar.Render("↑↓/jk navigate • Enter select • Esc cancel")

	// Build the modal
	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		list,
		"",
		help,
	)

	// Center the modal
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(m.Width / 2).
		Align(lipgloss.Left)

	modal := modalStyle.Render(content)

	// Center on screen
	return lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		modal,
	)
}

// renderInheritSelectOverlay renders the inherited search selection modal
func (m Model) renderInheritSelectOverlay() string {
	// Title
	title := m.Styles.SidebarTitle.Render("Select Inherited Searches")
	subtitle := lipgloss.NewStyle().Foreground(ColorMuted).Render("Toggle search templates to inherit")

	// Search list with checkboxes
	items := make([]string, 0, len(m.AvailableSearches))
	for i, search := range m.AvailableSearches {
		style := m.Styles.LogEntry
		if i == m.InheritCursor {
			style = m.Styles.LogSelected
		}

		// Checkbox indicator
		checkbox := "[ ]"
		if m.ActiveSearches[search] {
			checkbox = "[✓]"
		}

		items = append(items, style.Render(fmt.Sprintf("  %s %s", checkbox, search)))
	}

	list := strings.Join(items, "\n")

	// Help text
	help := m.Styles.HelpBar.Render("↑↓/jk navigate • Space toggle • Enter confirm • Esc cancel")

	// Build the modal
	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		subtitle,
		"",
		list,
		"",
		help,
	)

	// Center the modal
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(m.Width / 2).
		Align(lipgloss.Left)

	modal := modalStyle.Render(content)

	// Center on screen
	return lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		modal,
	)
}

// renderTabs renders the tab bar
func (m Model) renderTabs() string {
	if len(m.Tabs) == 0 {
		return m.Styles.TabBar.Width(m.Width).Render("No tabs")
	}

	var tabs []string
	for i, tab := range m.Tabs {
		name := tab.Name
		if tab.Loading {
			name += " ⏳"
		}
		if tab.Error != nil {
			name += " ❌"
		}

		if i == m.ActiveTab {
			tabs = append(tabs, m.Styles.TabActive.Render(name))
		} else {
			tabs = append(tabs, m.Styles.TabInactive.Render(name))
		}
	}

	tabRow := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	return m.Styles.TabBar.Width(m.Width).Render(tabRow)
}

// renderMainArea renders the log list and optional sidebar
func (m Model) renderMainArea() string {
	if !m.DetailsVisible {
		return m.Viewport.View()
	}

	// Split view
	listView := m.Viewport.View()

	// Render sidebar with tab-style header
	sidebarContent := m.renderSidebarWithTabs()
	sidebarView := m.Styles.Sidebar.Height(m.Viewport.Height).Render(sidebarContent)

	return lipgloss.JoinHorizontal(lipgloss.Top, listView, sidebarView)
}

// renderSidebarWithTabs renders the sidebar with tab-style mode selector
func (m Model) renderSidebarWithTabs() string {
	// Tab header styles
	activeTab := lipgloss.NewStyle().
		Foreground(ColorText).
		Background(ColorPrimary).
		Padding(0, 1).
		Bold(true)
	inactiveTab := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Padding(0, 1)

	// Render tabs
	var entryTab, jsonTab, fieldsTab string
	switch m.SidebarMode {
	case SidebarModeEntry:
		entryTab = activeTab.Render("Entry")
		jsonTab = inactiveTab.Render("JSON")
		fieldsTab = inactiveTab.Render("Fields")
	case SidebarModeJSON:
		entryTab = inactiveTab.Render("Entry")
		jsonTab = activeTab.Render("JSON")
		fieldsTab = inactiveTab.Render("Fields")
	default:
		entryTab = inactiveTab.Render("Entry")
		jsonTab = inactiveTab.Render("JSON")
		fieldsTab = activeTab.Render("Fields")
	}

	// Tab bar with help hint
	tabBar := lipgloss.JoinHorizontal(lipgloss.Center, entryTab, " ", jsonTab, " ", fieldsTab)
	tabHint := lipgloss.NewStyle().Foreground(ColorMuted).Render(" (F)")
	header := tabBar + tabHint

	// Content
	content := m.SidebarVP.View()

	return lipgloss.JoinVertical(lipgloss.Left, header, "", content)
}

// renderSearchFooter renders the chip-based search bar and help text
func (m Model) renderSearchFooter() string {
	parts := make([]string, 0, 2)

	// Search bar (chip-based)
	parts = append(parts, m.SearchBar.View())

	// Help text
	helpText := "↑↓ navigate • / search • w wrap • I inherits • Tab autocomplete • Enter sidebar • F fields • ? help • q quit"
	if m.ShowHelp {
		helpText = "↑↓/jk nav • PgUp/PgDn scroll • Tab/Shift+Tab tabs • Ctrl+T new • Ctrl+W close • w wrap • I inherits • [ ] resize • Enter sidebar • F fields • Esc clear • q quit"
	}
	parts = append(parts, m.Styles.HelpBar.Render(helpText))

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
