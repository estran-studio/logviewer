package factory_test

import (
	"context"
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/log/factory"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

// MockLogBackend implements client.LogBackend for testing
type MockLogBackend struct {
	LastSearch *client.LogSearch
	OnGet      func(search *client.LogSearch) (client.LogSearchResult, error)
	OnValues   func(search *client.LogSearch, fields []string) (map[string][]string, error)
}

func (m *MockLogBackend) Get(_ context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
	m.LastSearch = search
	if m.OnGet != nil {
		return m.OnGet(search)
	}
	return nil, nil
}

func (m *MockLogBackend) GetFieldValues(_ context.Context, search *client.LogSearch, fields []string) (map[string][]string, error) {
	m.LastSearch = search
	if m.OnValues != nil {
		return m.OnValues(search, fields)
	}
	return nil, nil
}

// MockLogBackendFactory implements factory.LogBackendFactory
type MockLogBackendFactory struct {
	Backends map[string]client.LogBackend
}

func (m *MockLogBackendFactory) Get(name string) (*client.LogBackend, error) {
	if b, ok := m.Backends[name]; ok {
		return &b, nil
	}
	return nil, nil
}

func TestSearchFactory_GetSearchResult(t *testing.T) {
	mockBackend := &MockLogBackend{}
	mockClientFactory := &MockLogBackendFactory{
		Backends: map[string]client.LogBackend{
			"test-client": mockBackend,
		},
	}

	cfg := config.ContextConfig{
		Clients: config.Clients{
			"test-client": config.Client{
				Type: "local",
				Options: ty.MI{
					"client-opt": "val1",
				},
			},
		},
		Contexts: config.Contexts{
			"test-ctx": config.SearchContext{
				Client: "test-client",
				Search: client.LogSearch{
					Options: ty.MI{
						"search-opt": "val2",
					},
				},
			},
		},
	}

	f, _ := factory.GetLogSearchFactory(mockClientFactory, cfg)

	t.Run("basic resolution and merging", func(t *testing.T) {
		_, err := f.GetSearchResult(context.Background(), "test-ctx", nil, client.LogSearch{}, nil)
		assert.NoError(t, err)

		// Verify client options were merged into search options
		assert.NotNil(t, mockBackend.LastSearch)
		assert.Equal(t, "val1", mockBackend.LastSearch.Options["client-opt"])
		assert.Equal(t, "val2", mockBackend.LastSearch.Options["search-opt"])
	})

	t.Run("search options override client options", func(t *testing.T) {
		overrideSearch := client.LogSearch{
			Options: ty.MI{
				"client-opt": "overridden",
			},
		}
		_, err := f.GetSearchResult(context.Background(), "test-ctx", nil, overrideSearch, nil)
		assert.NoError(t, err)

		assert.Equal(t, "overridden", mockBackend.LastSearch.Options["client-opt"])
	})
}

func TestSearchFactory_GetFieldValues(t *testing.T) {
	mockBackend := &MockLogBackend{
		OnValues: func(search *client.LogSearch, fields []string) (map[string][]string, error) {
			return map[string][]string{"level": {"INFO"}}, nil
		},
	}
	mockClientFactory := &MockLogBackendFactory{
		Backends: map[string]client.LogBackend{
			"test-client": mockBackend,
		},
	}

	cfg := config.ContextConfig{
		Clients: config.Clients{
			"test-client": config.Client{Type: "local"},
		},
		Contexts: config.Contexts{
			"test-ctx": config.SearchContext{Client: "test-client"},
		},
	}

	f, _ := factory.GetLogSearchFactory(mockClientFactory, cfg)

	results, err := f.GetFieldValues(context.Background(), "test-ctx", nil, client.LogSearch{}, []string{"level"}, nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{"INFO"}, results["level"])
}

func TestSearchFactory_GetSearchContext(t *testing.T) {
	mockClientFactory := &MockLogBackendFactory{}
	cfg := config.ContextConfig{
		Contexts: config.Contexts{
			"test-ctx": config.SearchContext{
				Client:      "test-client",
				Description: "test desc",
			},
		},
	}

	f, _ := factory.GetLogSearchFactory(mockClientFactory, cfg)

	ctx, err := f.GetSearchContext(context.Background(), "test-ctx", nil, client.LogSearch{}, nil)
	assert.NoError(t, err)
	assert.Equal(t, "test-client", ctx.Client)
	assert.Equal(t, "test desc", ctx.Description)
}
