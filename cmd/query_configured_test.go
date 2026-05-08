package cmd

import (
	"context"
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

// MockSearchFactory for testing ConfiguredLogClient
type MockSearchFactory struct {
	OnGetSearchResult func(ctx context.Context, contextID string, search client.LogSearch) (client.LogSearchResult, error)
	OnGetFieldValues  func(ctx context.Context, contextID string, search client.LogSearch, fields []string) (map[string][]string, error)
}

func (m *MockSearchFactory) GetSearchResult(ctx context.Context, contextID string, inherits []string, logSearch client.LogSearch, runtimeVars map[string]string) (client.LogSearchResult, error) {
	if m.OnGetSearchResult != nil {
		return m.OnGetSearchResult(ctx, contextID, logSearch)
	}
	return nil, nil
}

func (m *MockSearchFactory) GetSearchContext(ctx context.Context, contextID string, inherits []string, logSearch client.LogSearch, runtimeVars map[string]string) (*config.SearchContext, error) {
	return nil, nil
}

func (m *MockSearchFactory) GetFieldValues(ctx context.Context, contextID string, inherits []string, logSearch client.LogSearch, fields []string, runtimeVars map[string]string) (map[string][]string, error) {
	if m.OnGetFieldValues != nil {
		return m.OnGetFieldValues(ctx, contextID, logSearch, fields)
	}
	return nil, nil
}

type MockResult struct {
	Entries []client.LogEntry
	Fields  ty.UniSet[string]
}

func (m *MockResult) GetSearch() *client.LogSearch { return &client.LogSearch{} }
func (m *MockResult) GetEntries(_ context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	return m.Entries, nil, nil
}
func (m *MockResult) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	return m.Fields, nil, nil
}
func (m *MockResult) GetPaginationInfo() *client.PaginationInfo { return nil }
func (m *MockResult) Err() <-chan error { return nil }

func TestConfiguredLogClient_Query(t *testing.T) {
	mockFactory := &MockSearchFactory{
		OnGetSearchResult: func(ctx context.Context, contextID string, search client.LogSearch) (client.LogSearchResult, error) {
			return &MockResult{
				Entries: []client.LogEntry{{Message: "log from " + contextID}},
			}, nil
		},
	}

	cli := &ConfiguredLogClient{
		Factory:    mockFactory,
		ContextIDs: []string{"ctx1", "ctx2"},
	}

	entries, err := cli.Query(context.Background(), client.LogSearch{})
	assert.NoError(t, err)
	assert.Len(t, entries, 2)
	assert.Contains(t, []string{entries[0].Message, entries[1].Message}, "log from ctx1")
	assert.Contains(t, []string{entries[0].Message, entries[1].Message}, "log from ctx2")
}

func TestConfiguredLogClient_GetFields(t *testing.T) {
	mockFactory := &MockSearchFactory{
		OnGetSearchResult: func(ctx context.Context, contextID string, search client.LogSearch) (client.LogSearchResult, error) {
			fields := make(ty.UniSet[string])
			fields.Add("field-"+contextID, "val")
			return &MockResult{Fields: fields}, nil
		},
	}

	cli := &ConfiguredLogClient{
		Factory:    mockFactory,
		ContextIDs: []string{"ctx1", "ctx2"},
	}

	fields, err := cli.GetFields(context.Background(), client.LogSearch{})
	assert.NoError(t, err)
	assert.Contains(t, fields, "field-ctx1")
	assert.Contains(t, fields, "field-ctx2")
}

func TestConfiguredLogClient_GetValues(t *testing.T) {
	mockFactory := &MockSearchFactory{
		OnGetFieldValues: func(ctx context.Context, contextID string, search client.LogSearch, fields []string) (map[string][]string, error) {
			return map[string][]string{
				fields[0]: {"val-from-" + contextID},
			}, nil
		},
	}

	cli := &ConfiguredLogClient{
		Factory:    mockFactory,
		ContextIDs: []string{"ctx1", "ctx2"},
	}

	values, err := cli.GetValues(context.Background(), client.LogSearch{}, "level")
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"val-from-ctx1", "val-from-ctx2"}, values)
}

func TestResolveLogClient_AdHoc(t *testing.T) {
	// Setup global flags for ad-hoc
	cmd = "tail -f"
	defer func() { cmd = "" }()

	cli, _, err := resolveLogClient()
	assert.NoError(t, err)
	assert.NotNil(t, cli)
	// Should be a BackendAdapter wrapping Local client
	assert.IsType(t, &client.BackendAdapter{}, cli)
}
