package elk

import (
	"context"
	"testing"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchResult_GetPaginationInfo(t *testing.T) {
	t.Run("no size set, no pagination", func(t *testing.T) {
		search := &client.LogSearch{}
		result := SearchResult{search: search}
		assert.Nil(t, result.GetPaginationInfo())
	})

	t.Run("results less than size, no more pages", func(t *testing.T) {
		search := &client.LogSearch{Size: ty.Opt[int]{Value: 10, Set: true}}
		result := SearchResult{
			search: search,
			result: Hits{Hits: make([]Hit, 5)},
		}
		assert.Nil(t, result.GetPaginationInfo())
	})

	t.Run("results equal size, more pages", func(t *testing.T) {
		search := &client.LogSearch{Size: ty.Opt[int]{Value: 10, Set: true}}
		result := SearchResult{
			search: search,
			result: Hits{Hits: make([]Hit, 10)},
		}
		paginationInfo := result.GetPaginationInfo()
		assert.NotNil(t, paginationInfo)
		assert.True(t, paginationInfo.HasMore)
		assert.Equal(t, "10", paginationInfo.NextPageToken)
	})

	t.Run("with existing page token", func(t *testing.T) {
		search := &client.LogSearch{
			Size:      ty.Opt[int]{Value: 10, Set: true},
			PageToken: ty.Opt[string]{Value: "10", Set: true},
		}
		result := SearchResult{
			search:        search,
			result:        Hits{Hits: make([]Hit, 10)},
			CurrentOffset: 10,
		}
		paginationInfo := result.GetPaginationInfo()
		assert.NotNil(t, paginationInfo)
		assert.True(t, paginationInfo.HasMore)
		assert.Equal(t, "20", paginationInfo.NextPageToken)
	})

	t.Run("invalid page token", func(t *testing.T) {
		search := &client.LogSearch{
			Size:      ty.Opt[int]{Value: 10, Set: true},
			PageToken: ty.Opt[string]{Value: "invalid", Set: true},
		}
		result := SearchResult{
			search: search,
			result: Hits{Hits: make([]Hit, 10)},
		}
		paginationInfo := result.GetPaginationInfo()
		assert.NotNil(t, paginationInfo)
		assert.True(t, paginationInfo.HasMore)
		assert.Equal(t, "10", paginationInfo.NextPageToken)
	})
}

// mockElkLogClient implements client.LogBackend for testing refresh functionality
type mockElkLogClient struct {
	getCalls     int
	lastSearch   *client.LogSearch
	returnResult Hits
	returnError  error
}

func (m *mockElkLogClient) Get(_ context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
	m.getCalls++
	m.lastSearch = search
	if m.returnError != nil {
		return nil, m.returnError
	}
	return SearchResult{
		client:  m,
		search:  search,
		result:  m.returnResult,
		ErrChan: make(chan error, 1),
	}, nil
}

func (m *mockElkLogClient) GetFieldValues(_ context.Context, _ *client.LogSearch, _ []string) (map[string][]string, error) {
	return nil, nil
}

func TestSearchResult_onChange(t *testing.T) {
	t.Run("Returns nil when refresh duration is empty", func(t *testing.T) {
		search := &client.LogSearch{}
		result := SearchResult{
			search:  search,
			ErrChan: make(chan error, 1),
		}

		ch, err := result.onChange(context.Background())
		assert.NoError(t, err)
		assert.Nil(t, ch)
	})

	t.Run("Returns error for invalid duration", func(t *testing.T) {
		search := &client.LogSearch{
			Refresh: client.RefreshOptions{
				Duration: ty.Opt[string]{Value: "invalid", Set: true},
			},
		}
		result := SearchResult{
			search:  search,
			ErrChan: make(chan error, 1),
		}

		_, err := result.onChange(context.Background())
		assert.Error(t, err)
	})

	t.Run("Uses time.Now when Lte is empty", func(t *testing.T) {
		// This tests the bug fix: previously, parsing Lte when empty would fail
		mockClient := &mockElkLogClient{
			returnResult: Hits{
				Hits: []Hit{
					{
						Source: ty.MI{
							"message":    "test message",
							"@timestamp": time.Now().Format(time.RFC3339),
						},
					},
				},
			},
		}

		search := &client.LogSearch{
			Refresh: client.RefreshOptions{
				Duration: ty.Opt[string]{Value: "100ms", Set: true},
			},
			// Note: Range.Lte is NOT set (empty) - this was the bug
		}
		result := SearchResult{
			client:  mockClient,
			search:  search,
			ErrChan: make(chan error, 1),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()

		ch, err := result.onChange(ctx)
		require.NoError(t, err)
		require.NotNil(t, ch)

		// Wait for at least one polling cycle
		select {
		case entries := <-ch:
			// Should receive entries without error
			assert.NotNil(t, entries)
		case <-ctx.Done():
			// Context timeout means poll was attempted
		}

		// Verify the mock was called (polling occurred)
		assert.GreaterOrEqual(t, mockClient.getCalls, 1)
	})

	t.Run("Uses Lte value when provided", func(t *testing.T) {
		mockClient := &mockElkLogClient{
			returnResult: Hits{
				Hits: []Hit{
					{
						Source: ty.MI{
							"message":    "test message",
							"@timestamp": time.Now().Format(time.RFC3339),
						},
					},
				},
			},
		}

		lteTime := time.Now().Add(-1 * time.Minute)
		search := &client.LogSearch{
			Refresh: client.RefreshOptions{
				Duration: ty.Opt[string]{Value: "100ms", Set: true},
			},
		}
		search.Range.Lte.S(lteTime.Format(time.RFC3339))

		result := SearchResult{
			client:  mockClient,
			search:  search,
			ErrChan: make(chan error, 1),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()

		ch, err := result.onChange(ctx)
		require.NoError(t, err)
		require.NotNil(t, ch)

		// Wait for at least one polling cycle
		select {
		case <-ch:
			// Good, received entries
		case <-ctx.Done():
			// Timeout is ok
		}

		// Verify the mock client received updated search with new Gte/Lte
		if mockClient.lastSearch != nil {
			// After first poll, Gte should be set to lastLte + 1 second
			assert.NotEmpty(t, mockClient.lastSearch.Range.Gte.Value)
			assert.NotEmpty(t, mockClient.lastSearch.Range.Lte.Value)
		}
	})

	t.Run("Uses Lte value with nanoseconds", func(t *testing.T) {
		mockClient := &mockElkLogClient{
			returnResult: Hits{
				Hits: []Hit{},
			},
		}

		lteTime := time.Now().Add(-1 * time.Minute)
		search := &client.LogSearch{
			Refresh: client.RefreshOptions{
				Duration: ty.Opt[string]{Value: "100ms", Set: true},
			},
		}
		// Use RFC3339Nano format
		search.Range.Lte.S(lteTime.Format(time.RFC3339Nano))

		result := SearchResult{
			client:  mockClient,
			search:  search,
			ErrChan: make(chan error, 1),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()

		ch, err := result.onChange(ctx)
		require.NoError(t, err)
		require.NotNil(t, ch)

		select {
		case <-ch:
		case <-ctx.Done():
		}

		assert.GreaterOrEqual(t, mockClient.getCalls, 1)

		expectedGte := lteTime.Add(time.Second).Format(time.RFC3339)
		assert.Equal(t, expectedGte, mockClient.lastSearch.Range.Gte.Value)
	})

	t.Run("Uses custom timestamp format", func(t *testing.T) {
		mockClient := &mockElkLogClient{
			returnResult: Hits{
				Hits: []Hit{},
			},
		}

		customFormat := "2006/01/02 15:04:05"
		lteTime := time.Now().Add(-1 * time.Minute)
		search := &client.LogSearch{
			Refresh: client.RefreshOptions{
				Duration: ty.Opt[string]{Value: "100ms", Set: true},
			},
			Options: ty.MI{
				"timestampFormat": customFormat,
			},
		}
		search.Range.Lte.S(lteTime.Format(customFormat))

		result := SearchResult{
			client:  mockClient,
			search:  search,
			ErrChan: make(chan error, 1),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()

		ch, err := result.onChange(ctx)
		require.NoError(t, err)
		require.NotNil(t, ch)

		select {
		case <-ch:
		case <-ctx.Done():
		}

		assert.GreaterOrEqual(t, mockClient.getCalls, 1)

		expectedGte := lteTime.Add(time.Second).Format(customFormat)
		assert.Equal(t, expectedGte, mockClient.lastSearch.Range.Gte.Value)
	})
}

func TestSearchResult_parseResults(t *testing.T) {
	t.Run("Parses hits correctly", func(t *testing.T) {
		timestamp := time.Now().Format(time.RFC3339Nano)
		result := SearchResult{
			search: &client.LogSearch{Options: ty.MI{"index": "test"}},
			result: Hits{
				Hits: []Hit{
					{
						Source: ty.MI{
							"message":    "test message 1",
							"@timestamp": timestamp,
							"level":      "INFO",
						},
					},
					{
						Source: ty.MI{
							"message":    "test message 2",
							"@timestamp": timestamp,
							"level":      "ERROR",
						},
					},
				},
			},
		}

		entries := result.parseResults()
		assert.Len(t, entries, 2)

		// Results are reversed (newest first)
		assert.Equal(t, "test message 2", entries[0].Message)
		assert.Equal(t, "test message 1", entries[1].Message)
	})

	t.Run("Handles missing message", func(t *testing.T) {
		timestamp := time.Now().Format(time.RFC3339Nano)
		result := SearchResult{
			search: &client.LogSearch{Options: ty.MI{"index": "test"}},
			result: Hits{
				Hits: []Hit{
					{
						Source: ty.MI{
							"@timestamp": timestamp,
							// message is missing
						},
					},
				},
			},
		}

		entries := result.parseResults()
		assert.Len(t, entries, 1)
		assert.Empty(t, entries[0].Message)
	})

	t.Run("Handles message as non-string", func(t *testing.T) {
		timestamp := time.Now().Format(time.RFC3339Nano)
		result := SearchResult{
			search: &client.LogSearch{Options: ty.MI{"index": "test"}},
			result: Hits{
				Hits: []Hit{
					{
						Source: ty.MI{
							"message":    12345, // Not a string
							"@timestamp": timestamp,
						},
					},
				},
			},
		}

		entries := result.parseResults()
		assert.Len(t, entries, 1)
		// Should handle gracefully (empty entry)
	})

	t.Run("Parses level field", func(t *testing.T) {
		timestamp := time.Now().Format(time.RFC3339Nano)
		result := SearchResult{
			search: &client.LogSearch{Options: ty.MI{"index": "test"}},
			result: Hits{
				Hits: []Hit{
					{
						Source: ty.MI{
							"message":    "log message",
							"@timestamp": timestamp,
							"level":      "WARN",
						},
					},
				},
			},
		}

		entries := result.parseResults()
		assert.Len(t, entries, 1)
		assert.Equal(t, "WARN", entries[0].Level)
	})
}

func TestSearchResult_GetSearch(t *testing.T) {
	search := &client.LogSearch{Follow: true}
	result := SearchResult{search: search}

	assert.Equal(t, search, result.GetSearch())
}

func TestSearchResult_Err(t *testing.T) {
	errChan := make(chan error, 1)
	result := SearchResult{ErrChan: errChan}

	assert.Equal(t, (<-chan error)(errChan), result.Err())
}
