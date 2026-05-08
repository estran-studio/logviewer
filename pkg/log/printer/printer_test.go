package printer

import (
	"bytes"
	"context"
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

// MockLogSearchResult is a mock implementation of client.LogSearchResult for testing purposes.
type MockLogSearchResult struct {
	search  *client.LogSearch
	entries []client.LogEntry
}

func (m *MockLogSearchResult) GetSearch() *client.LogSearch {
	return m.search
}

func (m *MockLogSearchResult) GetEntries(_ context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	return m.entries, nil, nil
}

func (m *MockLogSearchResult) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	return nil, nil, nil
}

func (m *MockLogSearchResult) GetPaginationInfo() *client.PaginationInfo {
	return nil
}

func (m *MockLogSearchResult) Err() <-chan error {
	return nil
}

func TestMessageRegex(t *testing.T) {
	tests := []struct {
		name           string
		logEntries     []client.LogEntry
		printerOptions client.PrinterOptions
		expectedOutput string
	}{
		{
			name: "MessageRegex removes prefix",
			logEntries: []client.LogEntry{
				{Message: "[INFO] this is a test"},
			},
			printerOptions: client.PrinterOptions{
				MessageRegex: ty.OptWrap("^\\[\\w+\\]\\s*(.*)$"),
				Template:     ty.OptWrap("{{.Message}}"),
			},
			expectedOutput: "this is a test\n",
		},
		{
			name: "MessageRegex with no match",
			logEntries: []client.LogEntry{
				{Message: "this is a test"},
			},
			printerOptions: client.PrinterOptions{
				MessageRegex: ty.OptWrap("^\\[\\w+\\]\\s*(.*)$"),
				Template:     ty.OptWrap("{{.Message}}"),
			},
			expectedOutput: "this is a test\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			search := &client.LogSearch{
				PrinterOptions: tt.printerOptions,
			}

			result := &MockLogSearchResult{
				search:  search,
				entries: tt.logEntries,
			}

			var buf bytes.Buffer
			_, err := WrapIoWritter(context.Background(), result, &buf, func() {}, func(_ error) {})

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedOutput, buf.String())
		})
	}
}
