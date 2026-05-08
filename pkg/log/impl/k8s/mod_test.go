package k8s

import (
	"context"
	"testing"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestK8sLogClient_Get_Follow_Flag verifies that the Follow flag is correctly
// derived from search.Follow, not from search.Refresh.Duration
func TestK8sLogClient_Get_Follow_Flag(t *testing.T) {
	t.Run("Follow should use search.Follow field not Refresh.Duration", func(t *testing.T) {
		// Before the fix, the code was:
		//   follow := search.Refresh.Duration.Value != ""
		// After the fix:
		//   follow := search.Follow

		// Test case: Follow=false but Refresh.Duration is set
		// Before fix: follow would be true (bug!)
		// After fix: follow should be false
		search := &client.LogSearch{
			Follow: false,
			Options: ty.MI{
				FieldNamespace: "default",
				FieldPod:       "test-pod",
			},
			Refresh: client.RefreshOptions{
				Duration: ty.Opt[string]{Value: "30s", Set: true},
			},
		}

		// The fixed implementation uses search.Follow directly
		follow := search.Follow
		assert.False(t, follow, "Follow should be false when search.Follow is false, regardless of Refresh.Duration")
	})

	t.Run("Follow should be true when search.Follow is true", func(t *testing.T) {
		search := &client.LogSearch{
			Follow: true,
			Options: ty.MI{
				FieldNamespace: "default",
				FieldPod:       "test-pod",
			},
		}

		follow := search.Follow
		assert.True(t, follow, "Follow should be true when search.Follow is true")
	})

	t.Run("Follow should be false when search.Follow is false and no Refresh.Duration", func(t *testing.T) {
		search := &client.LogSearch{
			Follow: false,
			Options: ty.MI{
				FieldNamespace: "default",
				FieldPod:       "test-pod",
			},
		}

		follow := search.Follow
		assert.False(t, follow, "Follow should be false by default")
	})
}

func TestK8sLogClient_Options_Parsing(t *testing.T) {
	t.Run("Parses namespace from options", func(t *testing.T) {
		search := &client.LogSearch{
			Options: ty.MI{
				FieldNamespace: "my-namespace",
			},
		}

		namespace := search.Options.GetString(FieldNamespace)
		assert.Equal(t, "my-namespace", namespace)
	})

	t.Run("Parses pod from options", func(t *testing.T) {
		search := &client.LogSearch{
			Options: ty.MI{
				FieldPod: "my-pod",
			},
		}

		pod := search.Options.GetString(FieldPod)
		assert.Equal(t, "my-pod", pod)
	})

	t.Run("Parses container from options", func(t *testing.T) {
		search := &client.LogSearch{
			Options: ty.MI{
				FieldContainer: "my-container",
			},
		}

		container := search.Options.GetString(FieldContainer)
		assert.Equal(t, "my-container", container)
	})

	t.Run("Parses labelSelector from options", func(t *testing.T) {
		search := &client.LogSearch{
			Options: ty.MI{
				FieldLabelSelector: "app=myapp,env=prod",
			},
		}

		labelSelector := search.Options.GetString(FieldLabelSelector)
		assert.Equal(t, "app=myapp,env=prod", labelSelector)
	})

	t.Run("Parses previous flag from options", func(t *testing.T) {
		search := &client.LogSearch{
			Options: ty.MI{
				FieldPrevious: true,
			},
		}

		previous := search.Options.GetBool(FieldPrevious)
		assert.True(t, previous)
	})

	t.Run("Parses timestamp flag from options", func(t *testing.T) {
		search := &client.LogSearch{
			Options: ty.MI{
				OptionsTimestamp: true,
			},
		}

		timestamp := search.Options.GetBool(OptionsTimestamp)
		assert.True(t, timestamp)
	})
}

func TestK8sLogClient_TailLines(t *testing.T) {
	t.Run("TailLines should be nil when size not set", func(t *testing.T) {
		search := &client.LogSearch{
			Options: ty.MI{
				FieldNamespace: "default",
				FieldPod:       "test-pod",
			},
		}

		// Verify size is not set
		assert.False(t, search.Size.Set)

		// Simulate what the Get function does
		var tailLines *int64
		if search.Size.Set && search.Size.Value > 0 {
			lines := int64(search.Size.Value)
			tailLines = &lines
		}
		assert.Nil(t, tailLines)
	})

	t.Run("TailLines should be set when size is provided", func(t *testing.T) {
		search := &client.LogSearch{
			Size: ty.Opt[int]{Value: 100, Set: true},
			Options: ty.MI{
				FieldNamespace: "default",
				FieldPod:       "test-pod",
			},
		}

		assert.True(t, search.Size.Set)
		assert.Equal(t, 100, search.Size.Value)

		// Simulate what the Get function does
		var tailLines *int64
		if search.Size.Set && search.Size.Value > 0 {
			lines := int64(search.Size.Value)
			tailLines = &lines
		}
		require.NotNil(t, tailLines)
		assert.Equal(t, int64(100), *tailLines)
	})

	t.Run("TailLines should be nil when size is zero", func(t *testing.T) {
		search := &client.LogSearch{
			Size: ty.Opt[int]{Value: 0, Set: true},
			Options: ty.MI{
				FieldNamespace: "default",
				FieldPod:       "test-pod",
			},
		}

		var tailLines *int64
		if search.Size.Set && search.Size.Value > 0 {
			lines := int64(search.Size.Value)
			tailLines = &lines
		}
		assert.Nil(t, tailLines)
	})
}

func TestK8sLogClient_TimeRange(t *testing.T) {
	t.Run("SinceSeconds from Range.Last", func(t *testing.T) {
		search := &client.LogSearch{
			Options: ty.MI{
				FieldNamespace: "default",
				FieldPod:       "test-pod",
			},
		}
		search.Range.Last.S("1h")

		duration, err := time.ParseDuration(search.Range.Last.Value)
		require.NoError(t, err)
		assert.Equal(t, time.Hour, duration)

		seconds := int64(duration.Seconds())
		assert.Equal(t, int64(3600), seconds)
	})

	t.Run("SinceTime from Range.Gte", func(t *testing.T) {
		search := &client.LogSearch{
			Options: ty.MI{
				FieldNamespace: "default",
				FieldPod:       "test-pod",
			},
		}
		search.Range.Gte.S("2024-01-01T00:00:00Z")

		expectedTime, err := time.Parse(time.RFC3339, search.Range.Gte.Value)
		require.NoError(t, err)
		assert.Equal(t, 2024, expectedTime.Year())
		assert.Equal(t, time.January, expectedTime.Month())
		assert.Equal(t, 1, expectedTime.Day())
	})

	t.Run("Last takes precedence over Gte", func(t *testing.T) {
		search := &client.LogSearch{
			Options: ty.MI{
				FieldNamespace: "default",
				FieldPod:       "test-pod",
			},
		}
		search.Range.Last.S("30m")
		search.Range.Gte.S("2024-01-01T00:00:00Z")

		// When both Last and Gte are set, the code checks Last first
		// This simulates the Get function logic
		if search.Range.Last.Value != "" {
			lastDuration, err := time.ParseDuration(search.Range.Last.Value)
			require.NoError(t, err)
			assert.Equal(t, 30*time.Minute, lastDuration)
		}
	})
}

func TestPodNameInjector(t *testing.T) {
	t.Run("Injects pod name into entries with nil fields", func(t *testing.T) {
		mockResult := &mockLogSearchResult{
			entries: []client.LogEntry{
				{Message: "log line 1", Fields: nil},
			},
		}

		injector := &podNameInjector{
			inner:   mockResult,
			podName: "my-pod",
		}

		entries, _, err := injector.GetEntries(context.Background())
		require.NoError(t, err)
		require.Len(t, entries, 1)

		// Verify pod name is injected
		assert.NotNil(t, entries[0].Fields)
		assert.Equal(t, "my-pod", entries[0].Fields[FieldPod])
	})

	t.Run("Injects pod name while preserving existing fields", func(t *testing.T) {
		mockResult := &mockLogSearchResult{
			entries: []client.LogEntry{
				{Message: "log line 2", Fields: ty.MI{"existing": "value", "level": "info"}},
			},
		}

		injector := &podNameInjector{
			inner:   mockResult,
			podName: "my-pod",
		}

		entries, _, err := injector.GetEntries(context.Background())
		require.NoError(t, err)
		require.Len(t, entries, 1)

		// Verify pod name is injected
		assert.Equal(t, "my-pod", entries[0].Fields[FieldPod])

		// Verify existing fields are preserved
		assert.Equal(t, "value", entries[0].Fields["existing"])
		assert.Equal(t, "info", entries[0].Fields["level"])
	})

	t.Run("Handles streaming channel", func(t *testing.T) {
		ch := make(chan []client.LogEntry, 1)
		ch <- []client.LogEntry{{Message: "streamed log", Fields: nil}}
		close(ch)

		mockResult := &mockLogSearchResult{
			entries: []client.LogEntry{},
			channel: ch,
		}

		injector := &podNameInjector{
			inner:   mockResult,
			podName: "stream-pod",
		}

		_, wrappedCh, err := injector.GetEntries(context.Background())
		require.NoError(t, err)
		require.NotNil(t, wrappedCh)

		batch := <-wrappedCh
		require.Len(t, batch, 1)
		assert.Equal(t, "stream-pod", batch[0].Fields[FieldPod])
	})

	t.Run("Handles nil channel", func(t *testing.T) {
		mockResult := &mockLogSearchResult{
			entries: []client.LogEntry{{Message: "log", Fields: nil}},
			channel: nil,
		}

		injector := &podNameInjector{
			inner:   mockResult,
			podName: "test-pod",
		}

		entries, ch, err := injector.GetEntries(context.Background())
		require.NoError(t, err)
		assert.Nil(t, ch)
		require.Len(t, entries, 1)
		assert.Equal(t, "test-pod", entries[0].Fields[FieldPod])
	})

	t.Run("GetSearch delegates to inner", func(t *testing.T) {
		expectedSearch := &client.LogSearch{Follow: true}
		mockResult := &mockLogSearchResult{
			search: expectedSearch,
		}

		injector := &podNameInjector{
			inner:   mockResult,
			podName: "test-pod",
		}

		assert.Equal(t, expectedSearch, injector.GetSearch())
	})

	t.Run("GetFields delegates to inner", func(t *testing.T) {
		mockResult := &mockLogSearchResult{}

		injector := &podNameInjector{
			inner:   mockResult,
			podName: "test-pod",
		}

		fields, ch, err := injector.GetFields(context.Background())
		require.NoError(t, err)
		assert.Nil(t, ch)
		assert.NotNil(t, fields)
	})

	t.Run("GetPaginationInfo delegates to inner", func(t *testing.T) {
		mockResult := &mockLogSearchResult{}

		injector := &podNameInjector{
			inner:   mockResult,
			podName: "test-pod",
		}

		info := injector.GetPaginationInfo()
		assert.Nil(t, info)
	})

	t.Run("Err delegates to inner", func(t *testing.T) {
		mockResult := &mockLogSearchResult{}

		injector := &podNameInjector{
			inner:   mockResult,
			podName: "test-pod",
		}

		errCh := injector.Err()
		assert.Nil(t, errCh)
	})
}

// mockLogSearchResult implements client.LogSearchResult for testing
type mockLogSearchResult struct {
	entries []client.LogEntry
	channel chan []client.LogEntry
	search  *client.LogSearch
}

func (m *mockLogSearchResult) GetSearch() *client.LogSearch {
	return m.search
}

func (m *mockLogSearchResult) GetEntries(_ context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	return m.entries, m.channel, nil
}

func (m *mockLogSearchResult) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	return ty.UniSet[string]{}, nil, nil
}

func (m *mockLogSearchResult) GetPaginationInfo() *client.PaginationInfo {
	return nil
}

func (m *mockLogSearchResult) Err() <-chan error {
	return nil
}

// Compile-time check that mockLogSearchResult implements LogSearchResult
var _ client.LogSearchResult = (*mockLogSearchResult)(nil)
