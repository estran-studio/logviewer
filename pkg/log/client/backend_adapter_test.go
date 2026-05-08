package client_test

import (
	"context"
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

// MockLogBackend for testing Adapter
type MockLogBackend struct {
	OnGet            func(ctx context.Context, search *client.LogSearch) (client.LogSearchResult, error)
	OnGetFieldValues func(ctx context.Context, search *client.LogSearch, fields []string) (map[string][]string, error)
}

func (m *MockLogBackend) Get(ctx context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
	if m.OnGet != nil {
		return m.OnGet(ctx, search)
	}
	return nil, nil
}

func (m *MockLogBackend) GetFieldValues(ctx context.Context, search *client.LogSearch, fields []string) (map[string][]string, error) {
	if m.OnGetFieldValues != nil {
		return m.OnGetFieldValues(ctx, search, fields)
	}
	return nil, nil
}

// Reuse MockLogSearchResult from multi_search_result_test.go (it's in the same package client_test)
// Need to add GetFields support to it if not present/sufficient.
// In multi_search_result_test.go it returns nil, nil, nil. I might need to override it or create a specific one.

type AdapterMockResult struct {
	Entries    []client.LogEntry
	EntriesCh  chan []client.LogEntry
	Fields     ty.UniSet[string]
	FieldsCh   chan ty.UniSet[string]
}

func (m *AdapterMockResult) GetSearch() *client.LogSearch { return &client.LogSearch{} }
func (m *AdapterMockResult) GetEntries(_ context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	return m.Entries, m.EntriesCh, nil
}
func (m *AdapterMockResult) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	return m.Fields, m.FieldsCh, nil
}
func (m *AdapterMockResult) GetPaginationInfo() *client.PaginationInfo { return nil }
func (m *AdapterMockResult) Err() <-chan error { return nil }

func TestBackendAdapter_Query(t *testing.T) {
	// Setup: Backend returns some initial entries AND some streamed entries
	ch := make(chan []client.LogEntry, 1)
	
	backend := &MockLogBackend{
		OnGet: func(ctx context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
			return &AdapterMockResult{
				Entries: []client.LogEntry{{Message: "initial"}},
				EntriesCh: ch,
			}, nil
		},
	}

	adapter := client.NewBackendAdapter(backend)

	// Pump stream in background
	go func() {
		ch <- []client.LogEntry{{Message: "streamed"}}
		close(ch)
	}()

	results, err := adapter.Query(context.Background(), client.LogSearch{})
	assert.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "initial", results[0].Message)
	assert.Equal(t, "streamed", results[1].Message)
}

func TestBackendAdapter_GetFields(t *testing.T) {
	// Setup: Backend returns initial fields AND streamed fields
	ch := make(chan ty.UniSet[string], 1)
	initialFields := make(ty.UniSet[string])
	initialFields.Add("level", "INFO")

	backend := &MockLogBackend{
		OnGet: func(ctx context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
			return &AdapterMockResult{
				Fields:   initialFields,
				FieldsCh: ch,
			}, nil
		},
	}

	adapter := client.NewBackendAdapter(backend)

	// Pump stream
	go func() {
		streamedFields := make(ty.UniSet[string])
		streamedFields.Add("service", "api")
		ch <- streamedFields
		close(ch)
	}()

	fields, err := adapter.GetFields(context.Background(), client.LogSearch{})
	assert.NoError(t, err)
	
	// Check results (map[string][]string)
	assert.Contains(t, fields, "level")
	assert.Contains(t, fields, "service")
	assert.Contains(t, fields["level"], "INFO")
	assert.Contains(t, fields["service"], "api")
}

func TestBackendAdapter_GetValues(t *testing.T) {
	backend := &MockLogBackend{
		OnGetFieldValues: func(ctx context.Context, search *client.LogSearch, fields []string) (map[string][]string, error) {
			assert.Equal(t, "level", fields[0])
			return map[string][]string{
				"level": {"INFO", "ERROR"},
			}, nil
		},
	}

	adapter := client.NewBackendAdapter(backend)
	values, err := adapter.GetValues(context.Background(), client.LogSearch{}, "level")
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"INFO", "ERROR"}, values)
}

func TestBackendAdapter_GetValues_Empty(t *testing.T) {
	backend := &MockLogBackend{
		OnGetFieldValues: func(ctx context.Context, search *client.LogSearch, fields []string) (map[string][]string, error) {
			return map[string][]string{}, nil
		},
	}

	adapter := client.NewBackendAdapter(backend)
	values, err := adapter.GetValues(context.Background(), client.LogSearch{}, "unknown")
	assert.NoError(t, err)
	assert.Empty(t, values)
}
