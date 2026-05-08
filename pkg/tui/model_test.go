// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"context"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	tea "github.com/charmbracelet/bubbletea"
)

// MockSearchResult implements client.LogSearchResult
type MockSearchResult struct {
	Search *client.LogSearch
}

func (m *MockSearchResult) GetSearch() *client.LogSearch {
	return m.Search
}

func (m *MockSearchResult) GetEntries(_ context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	return []client.LogEntry{}, nil, nil
}

func (m *MockSearchResult) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	return nil, nil, nil
}

func (m *MockSearchResult) GetPaginationInfo() *client.PaginationInfo {
	return nil
}

func (m *MockSearchResult) Err() <-chan error {
	return nil
}

// Tests TestModelUpdate_LogEntryMsg_SanitizesFields and TestModelUpdate_LogEntryMsg_PreservesOtherFields
// were removed because auto-population of search bar from backend response was removed.
// See comment in model.go: "NOTE: Auto-population of search bar from context config was removed."

// Ensure Tea.Msg interface is satisfied (implied, but good practice)
var _ tea.Msg = LogEntryMsg{}
