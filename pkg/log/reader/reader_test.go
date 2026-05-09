package reader

import (
	"bufio"
	"context"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimestampExtraction(t *testing.T) {

	logResult := LogResult{
		entries: make([]client.LogEntry, 0),
		search: &client.LogSearch{
			Fields: ty.MS{},
		},
		fields: ty.UniSet[string]{},

		regexDate: regexp.MustCompile(ty.RegexTimestampFormat),
	}

	expectedTime, _ := time.Parse(ty.Format, "2024-06-24T15:27:29.669455265Z")
	entry, isParsed := logResult.parseBlock("\x01\x00\x00\x00\x00\x00\x00\x802024-06-24T15:27:29.669455265Z /docker-entrypoint.sh: /docker-entrypoint.d/ is not empty, will attempt to perform configuration")

	assert.Equal(t, true, isParsed)
	assert.Equal(t, "\x01\x00\x00\x00\x00\x00\x00\x80 /docker-entrypoint.sh: /docker-entrypoint.d/ is not empty, will attempt to perform configuration", entry.Message)
	assert.Equal(t, expectedTime, entry.Timestamp)

}

func TestLogResult_GetPaginationInfo(t *testing.T) {
	result := LogResult{}
	assert.Nil(t, result.GetPaginationInfo())
}

func TestLogResult_parseBlock(t *testing.T) {
	type fields struct {
		search                    *client.LogSearch
		kvRegexExtraction         *regexp.Regexp
		namedGroupRegexExtraction *regexp.Regexp
	}
	type args struct {
		line string
	}
	tests := []struct {
		name      string
		fields    fields
		args      args
		want      bool
		wantEntry *client.LogEntry
	}{
		{
			name: "Test no filtering without regex",
			fields: fields{
				search: &client.LogSearch{
					Fields: ty.MS{
						"level": "info",
					},
				},
			},
			args: args{
				line: "this is a log line",
			},
			want: true,
			wantEntry: &client.LogEntry{
				Message: "this is a log line",
				Fields:  ty.MI{},
			},
		},
		{
			name: "Test filtering with regex",
			fields: fields{
				search: &client.LogSearch{
					Fields: ty.MS{
						"level": "info",
					},
				},
				namedGroupRegexExtraction: regexp.MustCompile(`(?P<level>info|error)`),
			},
			args: args{
				line: "this is a info log line",
			},
			want: true,
			wantEntry: &client.LogEntry{
				Message: "this is a info log line",
				Fields: ty.MI{
					"level": "info",
				},
			},
		},
		{
			name: "Test filtering with regex no match",
			fields: fields{
				search: &client.LogSearch{
					Fields: ty.MS{
						"level": "error",
					},
				},
				namedGroupRegexExtraction: regexp.MustCompile(`(?P<level>info|error)`),
			},
			args: args{
				line: "this is a info log line",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lr := &LogResult{
				search:                    tt.fields.search,
				kvRegexExtraction:         tt.fields.kvRegexExtraction,
				namedGroupRegexExtraction: tt.fields.namedGroupRegexExtraction,
				entries:                   []client.LogEntry{},
				fields:                    ty.UniSet[string]{},
			}
			entry, got := lr.parseBlock(tt.args.line)
			if got != tt.want {
				t.Errorf("LogResult.parseBlock() = %v, want %v", got, tt.want)
			}
			if tt.wantEntry != nil {
				assert.Equal(t, tt.wantEntry.Message, entry.Message)
				assert.Equal(t, tt.wantEntry.Fields, entry.Fields)
			}
		})
	}
}

// nopCloser wraps an io.Reader to satisfy io.Closer
type nopCloser struct {
	io.Reader
	closed bool
}

func (n *nopCloser) Close() error {
	n.closed = true
	return nil
}

func TestLogResult_GetEntries_NonFollow(t *testing.T) {
	t.Run("Reads all entries when Follow is false", func(t *testing.T) {
		input := "line 1\nline 2\nline 3\n"
		reader := strings.NewReader(input)
		scanner := bufio.NewScanner(reader)
		closer := &nopCloser{Reader: reader}

		search := &client.LogSearch{
			Follow: false,
		}

		result, err := GetLogResult(search, scanner, closer)
		require.NoError(t, err)

		entries, ch, err := result.GetEntries(context.Background())
		require.NoError(t, err)

		assert.Len(t, entries, 3)
		assert.Nil(t, ch, "Channel should be nil when Follow is false")
		assert.True(t, closer.closed, "Closer should be called when Follow is false")
	})

	t.Run("Handles empty input", func(t *testing.T) {
		input := ""
		reader := strings.NewReader(input)
		scanner := bufio.NewScanner(reader)
		closer := &nopCloser{Reader: reader}

		search := &client.LogSearch{
			Follow: false,
		}

		result, err := GetLogResult(search, scanner, closer)
		require.NoError(t, err)

		entries, ch, err := result.GetEntries(context.Background())
		require.NoError(t, err)

		assert.Len(t, entries, 0)
		assert.Nil(t, ch)
	})
}

func TestLogResult_GetEntries_Follow(t *testing.T) {
	t.Run("Returns channel when Follow is true", func(t *testing.T) {
		// Use a pipe to simulate streaming input
		pr, pw := io.Pipe()
		scanner := bufio.NewScanner(pr)
		closer := &nopCloser{Reader: pr}

		search := &client.LogSearch{
			Follow: true,
		}

		result, err := GetLogResult(search, scanner, closer)
		require.NoError(t, err)

		entries, ch, err := result.GetEntries(context.Background())
		require.NoError(t, err)

		assert.Empty(t, entries, "Initial entries should be empty for Follow mode")
		assert.NotNil(t, ch, "Channel should be returned when Follow is true")

		// Write some data and verify it's received
		go func() {
			_, _ = pw.Write([]byte("streaming line 1\n"))
			_, _ = pw.Write([]byte("streaming line 2\n"))
			_ = pw.Close()
		}()

		received := make([]string, 0, 2)
		for batch := range ch {
			for _, entry := range batch {
				received = append(received, entry.Message)
			}
		}

		assert.Len(t, received, 2)
		assert.Contains(t, received, "streaming line 1")
		assert.Contains(t, received, "streaming line 2")
	})

	t.Run("Context cancellation stops streaming", func(t *testing.T) {
		pr, pw := io.Pipe()
		scanner := bufio.NewScanner(pr)
		closer := pr

		search := &client.LogSearch{
			Follow: true,
		}

		result, err := GetLogResult(search, scanner, closer)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		_, ch, err := result.GetEntries(ctx)
		require.NoError(t, err)
		require.NotNil(t, ch)

		// Write one line
		_, _ = pw.Write([]byte("first line\n"))

		// Cancel context
		cancel()

		// Close the writer to allow goroutine to exit
		_ = pw.Close()

		// Channel should eventually close
		select {
		case _, ok := <-ch:
			if !ok {
				// Channel closed as expected
				return
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for channel to close")
		}
	})
}

func TestLogResult_GetSearch(t *testing.T) {
	search := &client.LogSearch{Follow: true}
	result := LogResult{search: search}

	assert.Equal(t, search, result.GetSearch())
}

func TestLogResult_GetFields(t *testing.T) {
	fields := ty.UniSet[string]{"level": {"INFO", "ERROR"}}
	result := LogResult{fields: fields}

	returnedFields, ch, err := result.GetFields(context.Background())
	require.NoError(t, err)
	assert.Nil(t, ch)
	assert.Equal(t, fields, returnedFields)
}

func TestLogResult_Err(t *testing.T) {
	errChan := make(chan error, 1)
	result := LogResult{ErrChan: errChan}

	assert.Equal(t, (<-chan error)(errChan), result.Err())
}

func TestGetLogResult(t *testing.T) {
	t.Run("Creates result with valid search", func(t *testing.T) {
		input := "test line"
		reader := strings.NewReader(input)
		scanner := bufio.NewScanner(reader)
		closer := &nopCloser{Reader: reader}

		search := &client.LogSearch{}

		result, err := GetLogResult(search, scanner, closer)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, search, result.search)
	})

	t.Run("Compiles GroupRegex when provided", func(t *testing.T) {
		input := "test"
		reader := strings.NewReader(input)
		scanner := bufio.NewScanner(reader)
		closer := &nopCloser{Reader: reader}

		search := &client.LogSearch{}
		search.FieldExtraction.GroupRegex.S(`(?P<level>INFO|ERROR)`)

		result, err := GetLogResult(search, scanner, closer)
		require.NoError(t, err)
		assert.NotNil(t, result.namedGroupRegexExtraction)
	})

	t.Run("Returns error for invalid GroupRegex", func(t *testing.T) {
		input := "test"
		reader := strings.NewReader(input)
		scanner := bufio.NewScanner(reader)
		closer := &nopCloser{Reader: reader}

		search := &client.LogSearch{}
		search.FieldExtraction.GroupRegex.S(`[invalid regex`)

		_, err := GetLogResult(search, scanner, closer)
		assert.Error(t, err)
	})

	t.Run("Compiles KvRegex when provided", func(t *testing.T) {
		input := "test"
		reader := strings.NewReader(input)
		scanner := bufio.NewScanner(reader)
		closer := &nopCloser{Reader: reader}

		search := &client.LogSearch{}
		search.FieldExtraction.KvRegex.S(`(\w+)=(\w+)`)

		result, err := GetLogResult(search, scanner, closer)
		require.NoError(t, err)
		assert.NotNil(t, result.kvRegexExtraction)
	})

	t.Run("Returns error for invalid KvRegex", func(t *testing.T) {
		input := "test"
		reader := strings.NewReader(input)
		scanner := bufio.NewScanner(reader)
		closer := &nopCloser{Reader: reader}

		search := &client.LogSearch{}
		search.FieldExtraction.KvRegex.S(`[invalid`)

		_, err := GetLogResult(search, scanner, closer)
		assert.Error(t, err)
	})

	t.Run("Compiles TimestampRegex when provided", func(t *testing.T) {
		input := "test"
		reader := strings.NewReader(input)
		scanner := bufio.NewScanner(reader)
		closer := &nopCloser{Reader: reader}

		search := &client.LogSearch{}
		search.FieldExtraction.TimestampRegex.S(`\d{4}-\d{2}-\d{2}`)

		result, err := GetLogResult(search, scanner, closer)
		require.NoError(t, err)
		assert.NotNil(t, result.regexDate)
	})

	t.Run("Strips leading ^ from TimestampRegex for flexibility", func(t *testing.T) {
		input := "test"
		reader := strings.NewReader(input)
		scanner := bufio.NewScanner(reader)
		closer := &nopCloser{Reader: reader}

		search := &client.LogSearch{}
		search.FieldExtraction.TimestampRegex.S(`^\d{4}-\d{2}-\d{2}`)

		result, err := GetLogResult(search, scanner, closer)
		require.NoError(t, err)
		assert.NotNil(t, result.regexDate)
		// Should match timestamp even if not at start of line
		assert.True(t, result.regexDate.MatchString("prefix 2024-01-01"))
	})
}

func TestLogResult_processLine(t *testing.T) {
	t.Run("New entry when no timestamp regex", func(t *testing.T) {
		lr := &LogResult{
			search:  &client.LogSearch{},
			fields:  ty.UniSet[string]{},
			entries: []client.LogEntry{},
		}

		var pendingBlock strings.Builder
		var received []client.LogEntry
		onEntry := func(entry client.LogEntry) {
			received = append(received, entry)
		}

		lr.processLine("first line", &pendingBlock, onEntry)
		lr.processLine("second line", &pendingBlock, onEntry)
		lr.flushBlock(&pendingBlock, onEntry)

		assert.Len(t, received, 2)
	})

	t.Run("Multiline entry with timestamp regex", func(t *testing.T) {
		lr := &LogResult{
			search:    &client.LogSearch{},
			fields:    ty.UniSet[string]{},
			entries:   []client.LogEntry{},
			regexDate: regexp.MustCompile(`\d{4}-\d{2}-\d{2}`),
		}

		var pendingBlock strings.Builder
		var received []client.LogEntry
		onEntry := func(entry client.LogEntry) {
			received = append(received, entry)
		}

		// First line with timestamp
		lr.processLine("2024-01-01 First entry", &pendingBlock, onEntry)
		// Continuation without timestamp
		lr.processLine("  continuation of first entry", &pendingBlock, onEntry)
		// New entry with timestamp
		lr.processLine("2024-01-02 Second entry", &pendingBlock, onEntry)
		lr.flushBlock(&pendingBlock, onEntry)

		assert.Len(t, received, 2)
		assert.Contains(t, received[0].Message, "continuation")
	})
}

func TestLogResult_loadEntries(t *testing.T) {
	t.Run("Returns true when entries are loaded", func(t *testing.T) {
		input := "line 1\nline 2\n"
		reader := strings.NewReader(input)
		scanner := bufio.NewScanner(reader)

		lr := &LogResult{
			search:  &client.LogSearch{},
			scanner: scanner,
			fields:  ty.UniSet[string]{},
			entries: []client.LogEntry{},
		}

		result := lr.loadEntries()
		assert.True(t, result)
		assert.Len(t, lr.entries, 2)
	})

	t.Run("Returns false when no entries", func(t *testing.T) {
		input := ""
		reader := strings.NewReader(input)
		scanner := bufio.NewScanner(reader)

		lr := &LogResult{
			search:  &client.LogSearch{},
			scanner: scanner,
			fields:  ty.UniSet[string]{},
			entries: []client.LogEntry{},
		}

		result := lr.loadEntries()
		assert.False(t, result)
		assert.Len(t, lr.entries, 0)
	})
}

func TestParseTimestamp(t *testing.T) {
	t.Run("Parses RFC3339 format", func(t *testing.T) {
		ts, err := parseTimestamp("2024-01-15T10:30:45Z")
		require.NoError(t, err)
		assert.Equal(t, 2024, ts.Year())
		assert.Equal(t, time.January, ts.Month())
		assert.Equal(t, 15, ts.Day())
	})

	t.Run("Parses RFC3339Nano format (Docker timestamps)", func(t *testing.T) {
		ts, err := parseTimestamp("2026-05-09T19:55:26.055134611Z")
		require.NoError(t, err)
		assert.Equal(t, 2026, ts.Year())
		assert.Equal(t, 19, ts.Hour())
		assert.Equal(t, 55, ts.Minute())
		assert.Equal(t, 26, ts.Second())
	})

	t.Run("Parses ISO 8601 with T separator and no timezone (headscale)", func(t *testing.T) {
		ts, err := parseTimestamp("2026-05-09T19:55:26")
		require.NoError(t, err)
		assert.Equal(t, 2026, ts.Year())
		assert.Equal(t, 19, ts.Hour())
		assert.Equal(t, 55, ts.Minute())
		assert.Equal(t, 26, ts.Second())
	})

	t.Run("Parses ISO 8601 with T separator, fractional seconds, no timezone", func(t *testing.T) {
		ts, err := parseTimestamp("2026-05-09T19:55:26.055")
		require.NoError(t, err)
		assert.Equal(t, 2026, ts.Year())
		assert.Equal(t, 19, ts.Hour())
		assert.Equal(t, 55, ts.Minute())
		assert.Equal(t, 26, ts.Second())
	})

	t.Run("Parses nginx combined log format", func(t *testing.T) {
		ts, err := parseTimestamp("09/May/2026:20:41:33 +0000")
		require.NoError(t, err)
		assert.Equal(t, 2026, ts.Year())
		assert.Equal(t, time.May, ts.Month())
		assert.Equal(t, 9, ts.Day())
		assert.Equal(t, 20, ts.Hour())
		assert.Equal(t, 41, ts.Minute())
		assert.Equal(t, 33, ts.Second())
	})

	t.Run("Parses local time format", func(t *testing.T) {
		ts, err := parseTimestamp("2024-01-15 10:30:45.123")
		require.NoError(t, err)
		assert.Equal(t, 2024, ts.Year())
	})

	t.Run("Parses float64 Unix timestamp", func(t *testing.T) {
		unixTime := float64(1705315845.123456)
		ts, err := parseTimestamp(unixTime)
		require.NoError(t, err)
		assert.False(t, ts.IsZero())
	})

	t.Run("Returns error for unsupported type", func(t *testing.T) {
		_, err := parseTimestamp(12345)
		assert.Error(t, err)
	})
}

func TestLogResult_PreFiltered(t *testing.T) {
	t.Run("Skips filtering when __preFiltered__ is true", func(t *testing.T) {
		search := &client.LogSearch{
			Fields: ty.MS{"level": "ERROR"},
			Options: ty.MI{
				"__preFiltered__": true,
			},
		}
		search.FieldExtraction.JSON.S(true)

		lr := &LogResult{
			search:  search,
			fields:  ty.UniSet[string]{},
			entries: []client.LogEntry{},
		}

		// This line would normally be filtered out since level != ERROR
		// But with __preFiltered__, it should pass through
		entry, ok := lr.parseBlock(`{"message":"test","level":"INFO"}`)
		assert.True(t, ok, "Should pass through when preFiltered")
		assert.NotNil(t, entry)
	})

	t.Run("Applies filtering when __preFiltered__ is false", func(t *testing.T) {
		search := &client.LogSearch{
			Fields: ty.MS{"level": "ERROR"},
		}
		search.FieldExtraction.JSON.S(true)

		lr := &LogResult{
			search:  search,
			fields:  ty.UniSet[string]{},
			entries: []client.LogEntry{},
		}

		// This line should be filtered out since level != ERROR
		entry, ok := lr.parseBlock(`{"message":"test","level":"INFO"}`)
		assert.False(t, ok, "Should be filtered out when level doesn't match")
		assert.Nil(t, entry)
	})
}
