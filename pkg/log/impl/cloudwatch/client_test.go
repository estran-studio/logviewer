package cloudwatch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

// mockCWClient is a mock implementation of the CWClient interface.
type mockCWClient struct {
	StartQueryFunc      func(ctx context.Context, params *cloudwatchlogs.StartQueryInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StartQueryOutput, error)
	GetQueryResultsFunc func(ctx context.Context, params *cloudwatchlogs.GetQueryResultsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error)
	FilterLogEventsFunc func(ctx context.Context, params *cloudwatchlogs.FilterLogEventsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error)
}

func (m *mockCWClient) StartQuery(ctx context.Context, params *cloudwatchlogs.StartQueryInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StartQueryOutput, error) {
	return m.StartQueryFunc(ctx, params, optFns...)
}

func (m *mockCWClient) GetQueryResults(ctx context.Context, params *cloudwatchlogs.GetQueryResultsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error) {
	return m.GetQueryResultsFunc(ctx, params, optFns...)
}
func (m *mockCWClient) FilterLogEvents(ctx context.Context, params *cloudwatchlogs.FilterLogEventsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	if m.FilterLogEventsFunc != nil {
		return m.FilterLogEventsFunc(ctx, params, optFns...)
	}
	return &cloudwatchlogs.FilterLogEventsOutput{}, nil
}

func TestGetLogClient(t *testing.T) {
	// This test checks that providing a profile that doesn't exist returns an error.
	// This is the most we can test without a real AWS session or extensive mocking.
	t.Run("invalid profile", func(t *testing.T) {
		options := ty.MI{
			"profile": "this-profile-does-not-exist",
		}
		_, err := GetLogClient(options)
		assert.Error(t, err)
	})
}

func TestLogClient_Get(t *testing.T) {
	mockClient := &mockCWClient{
		StartQueryFunc: func(_ context.Context, params *cloudwatchlogs.StartQueryInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StartQueryOutput, error) {
			assert.Equal(t, "test-group", *params.LogGroupName)
			expectedQuery := "fields @timestamp, @message | filter level = 'error' | sort @timestamp desc | limit 100"
			assert.Equal(t, expectedQuery, *params.QueryString)
			// Validate that a duration Last overrides default (we didn't set Last here, so default ~1h window)
			assert.Greater(t, *params.EndTime, *params.StartTime)
			return &cloudwatchlogs.StartQueryOutput{
				QueryId: aws.String("test-query-id"),
			}, nil
		},
	}

	logClient := &LogClient{client: mockClient}

	search := &client.LogSearch{
		Fields: ty.MS{"level": "error"},
		Size:   ty.Opt[int]{Set: true, Value: 100},
		Options: ty.MI{
			"logGroupName": "test-group",
		},
	}

	result, err := logClient.Get(context.Background(), search)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	cwResult, ok := result.(*LogSearchResult)
	assert.True(t, ok)
	assert.Equal(t, "test-query-id", cwResult.queryID)
}

func TestLogSearchResult_GetEntries(t *testing.T) {
	mockClient := &mockCWClient{
		GetQueryResultsFunc: func(_ context.Context, _ *cloudwatchlogs.GetQueryResultsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error) {
			return &cloudwatchlogs.GetQueryResultsOutput{
				Status: types.QueryStatusComplete,
				Results: [][]types.ResultField{
					{
						{Field: aws.String("@timestamp"), Value: aws.String("2025-08-23 21:30:00.123")},
						{Field: aws.String("@message"), Value: aws.String("test message 1")},
						{Field: aws.String("level"), Value: aws.String("INFO")},
					},
					{
						{Field: aws.String("@timestamp"), Value: aws.String("2025-08-23 21:30:05.000")},
						{Field: aws.String("@message"), Value: aws.String("test message 2")},
						{Field: aws.String("level"), Value: aws.String("DEBUG")},
					},
				},
			}, nil
		},
	}

	searchResult := &LogSearchResult{
		client:  mockClient,
		queryID: "test-query-id",
		search:  &client.LogSearch{},
	}

	entries, _, err := searchResult.GetEntries(context.Background())
	assert.NoError(t, err)
	assert.Len(t, entries, 2)

	assert.Equal(t, "test message 1", entries[0].Message)
	assert.False(t, entries[0].Timestamp.IsZero())
	assert.Equal(t, 123000000, entries[0].Timestamp.Nanosecond())
	assert.Equal(t, "INFO", entries[0].Fields["level"])

	assert.Equal(t, "test message 2", entries[1].Message)
	assert.Equal(t, "DEBUG", entries[1].Fields["level"])
}

func TestCloudWatch_TimeRange_Last(t *testing.T) {
	mockClient := &mockCWClient{
		StartQueryFunc: func(_ context.Context, params *cloudwatchlogs.StartQueryInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StartQueryOutput, error) {
			// Expect approximately a 10m window
			windowMs := *params.EndTime - *params.StartTime
			assert.InDelta(t, 10*60*1000, windowMs, 5*1000) // allow 5s jitter
			return &cloudwatchlogs.StartQueryOutput{QueryId: aws.String("qid-last")}, nil
		},
	}
	c := &LogClient{client: mockClient}
	s := &client.LogSearch{Options: ty.MI{"logGroupName": "lg"}}
	s.Range.Last.S("10m")
	_, err := c.Get(context.Background(), s)
	assert.NoError(t, err)
}

func TestCloudWatch_TimeRange_GteLte(t *testing.T) {
	mockClient := &mockCWClient{
		StartQueryFunc: func(_ context.Context, params *cloudwatchlogs.StartQueryInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StartQueryOutput, error) {
			// We set explicit times; ensure they are respected
			expectedStart := time.Date(2025, 8, 23, 12, 0, 0, 0, time.UTC).UnixMilli()
			expectedEnd := time.Date(2025, 8, 23, 13, 0, 0, 0, time.UTC).UnixMilli()
			assert.Equal(t, expectedStart, *params.StartTime)
			assert.Equal(t, expectedEnd, *params.EndTime)
			return &cloudwatchlogs.StartQueryOutput{QueryId: aws.String("qid-abs")}, nil
		},
	}
	c := &LogClient{client: mockClient}
	s := &client.LogSearch{Options: ty.MI{"logGroupName": "lg"}}
	s.Range.Gte.S("2025-08-23T12:00:00Z")
	s.Range.Lte.S("2025-08-23T13:00:00Z")
	_, err := c.Get(context.Background(), s)
	assert.NoError(t, err)
}

func TestCloudWatch_GetFields(t *testing.T) {
	mockClient := &mockCWClient{
		GetQueryResultsFunc: func(_ context.Context, _ *cloudwatchlogs.GetQueryResultsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error) {
			return &cloudwatchlogs.GetQueryResultsOutput{
				Status: types.QueryStatusComplete,
				Results: [][]types.ResultField{{
					{Field: aws.String("@timestamp"), Value: aws.String("2025-08-23 21:30:00.123")},
					{Field: aws.String("@message"), Value: aws.String("log one")},
					{Field: aws.String("level"), Value: aws.String("INFO")},
					{Field: aws.String("service"), Value: aws.String("auth")},
				}, {
					{Field: aws.String("@timestamp"), Value: aws.String("2025-08-23 21:30:01.000")},
					{Field: aws.String("@message"), Value: aws.String("log two")},
					{Field: aws.String("level"), Value: aws.String("DEBUG")},
					{Field: aws.String("service"), Value: aws.String("auth")},
				}},
			}, nil
		},
	}
	sr := &LogSearchResult{client: mockClient, queryID: "qid-fields", search: &client.LogSearch{}}
	// Ensure entries loaded
	_, _, err := sr.GetEntries(context.Background())
	assert.NoError(t, err)
	fields, _, err := sr.GetFields(context.Background())
	assert.NoError(t, err)
	// Expect level INFO, DEBUG and service auth
	assert.Contains(t, fields["level"], "INFO")
	assert.Contains(t, fields["level"], "DEBUG")
	assert.Contains(t, fields["service"], "auth")
}

func TestLogClient_Get_WithPageToken(t *testing.T) {
	tokenTimestamp := time.Now().Format(time.RFC3339Nano)
	mockClient := &mockCWClient{
		StartQueryFunc: func(_ context.Context, params *cloudwatchlogs.StartQueryInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StartQueryOutput, error) {
			assert.Equal(t, "test-group", *params.LogGroupName)
			// Check that the query string contains the filter for the page token
			expectedFilter := fmt.Sprintf(" | filter @timestamp < timestamp('%s')", tokenTimestamp)
			assert.Contains(t, *params.QueryString, expectedFilter)
			return &cloudwatchlogs.StartQueryOutput{
				QueryId: aws.String("test-query-id-with-token"),
			}, nil
		},
	}

	logClient := &LogClient{client: mockClient}

	search := &client.LogSearch{
		PageToken: ty.Opt[string]{Set: true, Value: tokenTimestamp},
		Options: ty.MI{
			"logGroupName": "test-group",
		},
	}
	search.Range.Last.S("1h")

	result, err := logClient.Get(context.Background(), search)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	cwResult, ok := result.(*LogSearchResult)
	assert.True(t, ok)
	assert.Equal(t, "test-query-id-with-token", cwResult.queryID)
}

func TestLogSearchResult_GetPaginationInfo(t *testing.T) {
	t.Run("no size set", func(t *testing.T) {
		search := &client.LogSearch{}
		result := &LogSearchResult{search: search}
		assert.Nil(t, result.GetPaginationInfo())
	})

	t.Run("results less than size", func(t *testing.T) {
		search := &client.LogSearch{Size: ty.Opt[int]{Set: true, Value: 10}}
		result := &LogSearchResult{
			search:  search,
			entries: make([]client.LogEntry, 5),
		}
		assert.Nil(t, result.GetPaginationInfo())
	})

	t.Run("results equal size", func(t *testing.T) {
		search := &client.LogSearch{Size: ty.Opt[int]{Set: true, Value: 10}}
		entries := make([]client.LogEntry, 10)
		// The last entry determines the next token
		lastTimestamp := time.Now().Add(-1 * time.Minute)
		entries[9] = client.LogEntry{Timestamp: lastTimestamp}

		result := &LogSearchResult{
			search:  search,
			entries: entries,
		}

		info := result.GetPaginationInfo()
		assert.NotNil(t, info)
		assert.True(t, info.HasMore)
		assert.Equal(t, lastTimestamp.Format(time.RFC3339Nano), info.NextPageToken)
	})
}

// TestLogSearchResult_NoStreamingSupport documents that CloudWatch
// does not support streaming/Follow mode - it always returns nil for the channel.
// This is expected behavior since CloudWatch Logs Insights is query-based.
func TestLogSearchResult_NoStreamingSupport(t *testing.T) {
	t.Run("GetEntries returns nil channel - no streaming support", func(t *testing.T) {
		mockClient := &mockCWClient{
			GetQueryResultsFunc: func(_ context.Context, _ *cloudwatchlogs.GetQueryResultsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error) {
				return &cloudwatchlogs.GetQueryResultsOutput{
					Status:  types.QueryStatusComplete,
					Results: [][]types.ResultField{},
				}, nil
			},
		}

		// Even with Follow=true, CloudWatch should not return a streaming channel
		search := &client.LogSearch{
			Follow: true, // This should be ignored by CloudWatch
			Options: ty.MI{
				"logGroupName": "test-group",
			},
		}

		result := &LogSearchResult{
			client:  mockClient,
			queryID: "test-query-id",
			search:  search,
		}

		entries, ch, err := result.GetEntries(context.Background())
		assert.NoError(t, err)
		// entries may be nil or empty slice, both are acceptable
		assert.Empty(t, entries)
		assert.Nil(t, ch, "CloudWatch should not return a streaming channel (no Follow support)")
	})

	t.Run("Err returns nil - no async error channel", func(t *testing.T) {
		result := &LogSearchResult{
			search: &client.LogSearch{},
		}
		assert.Nil(t, result.Err(), "CloudWatch should return nil error channel")
	})
}

func TestLogSearchResult_PollingMechanism(t *testing.T) {
	t.Run("Polls until Complete status", func(t *testing.T) {
		pollCount := 0
		mockClient := &mockCWClient{
			GetQueryResultsFunc: func(_ context.Context, _ *cloudwatchlogs.GetQueryResultsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error) {
				pollCount++
				// Return Running for first 2 calls, then Complete
				if pollCount < 3 {
					return &cloudwatchlogs.GetQueryResultsOutput{
						Status: types.QueryStatusRunning,
					}, nil
				}
				return &cloudwatchlogs.GetQueryResultsOutput{
					Status: types.QueryStatusComplete,
					Results: [][]types.ResultField{
						{
							{Field: aws.String("@timestamp"), Value: aws.String("2025-01-01 00:00:00.000")},
							{Field: aws.String("@message"), Value: aws.String("test")},
						},
					},
				}, nil
			},
		}

		search := &client.LogSearch{
			Options: ty.MI{
				"logGroupName":           "test-group",
				"cloudwatchPollInterval": "1ms", // Fast polling for test
			},
		}

		result := &LogSearchResult{
			client:  mockClient,
			queryID: "test-query-id",
			search:  search,
		}

		entries, _, err := result.GetEntries(context.Background())
		assert.NoError(t, err)
		assert.Len(t, entries, 1)
		assert.GreaterOrEqual(t, pollCount, 3, "Should have polled at least 3 times")
	})

	t.Run("Context cancellation stops polling", func(t *testing.T) {
		pollCount := 0
		mockClient := &mockCWClient{
			GetQueryResultsFunc: func(_ context.Context, _ *cloudwatchlogs.GetQueryResultsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error) {
				pollCount++
				// Always return Running to force continuous polling
				return &cloudwatchlogs.GetQueryResultsOutput{
					Status: types.QueryStatusRunning,
				}, nil
			},
		}

		search := &client.LogSearch{
			Options: ty.MI{
				"logGroupName":           "test-group",
				"cloudwatchPollInterval": "1ms",
			},
		}

		result := &LogSearchResult{
			client:  mockClient,
			queryID: "test-query-id",
			search:  search,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		_, _, err := result.GetEntries(ctx)
		assert.Error(t, err, "Should return error when context is cancelled")
		assert.True(t, pollCount >= 1, "Should have polled at least once")
	})

	t.Run("Handles Failed status", func(t *testing.T) {
		mockClient := &mockCWClient{
			GetQueryResultsFunc: func(_ context.Context, _ *cloudwatchlogs.GetQueryResultsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error) {
				return &cloudwatchlogs.GetQueryResultsOutput{
					Status: types.QueryStatusFailed,
				}, nil
			},
		}

		result := &LogSearchResult{
			client:  mockClient,
			queryID: "test-query-id",
			search:  &client.LogSearch{Options: ty.MI{}},
		}

		entries, _, err := result.GetEntries(context.Background())
		assert.NoError(t, err) // Failed status is handled, no error returned
		assert.Empty(t, entries)
	})

	t.Run("Handles Cancelled status", func(t *testing.T) {
		mockClient := &mockCWClient{
			GetQueryResultsFunc: func(_ context.Context, _ *cloudwatchlogs.GetQueryResultsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error) {
				return &cloudwatchlogs.GetQueryResultsOutput{
					Status: types.QueryStatusCancelled,
				}, nil
			},
		}

		result := &LogSearchResult{
			client:  mockClient,
			queryID: "test-query-id",
			search:  &client.LogSearch{Options: ty.MI{}},
		}

		entries, _, err := result.GetEntries(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, entries)
	})
}

func TestLogSearchResult_GetSearch(t *testing.T) {
	search := &client.LogSearch{Follow: true}
	result := &LogSearchResult{search: search}

	assert.Equal(t, search, result.GetSearch())
}

func TestParseCloudWatchTimestamp(t *testing.T) {
	t.Run("Parses Insights format", func(t *testing.T) {
		ts, ok := parseCloudWatchTimestamp("2025-01-15 10:30:45.123")
		assert.True(t, ok)
		assert.Equal(t, 2025, ts.Year())
		assert.Equal(t, time.January, ts.Month())
		assert.Equal(t, 15, ts.Day())
	})

	t.Run("Parses RFC3339 format", func(t *testing.T) {
		ts, ok := parseCloudWatchTimestamp("2025-01-15T10:30:45Z")
		assert.True(t, ok)
		assert.Equal(t, 2025, ts.Year())
	})

	t.Run("Parses RFC3339Nano format", func(t *testing.T) {
		ts, ok := parseCloudWatchTimestamp("2025-01-15T10:30:45.123456789Z")
		assert.True(t, ok)
		assert.Equal(t, 2025, ts.Year())
	})

	t.Run("Parses epoch milliseconds", func(t *testing.T) {
		// 1705315845123 = 2024-01-15T10:30:45.123Z
		ts, ok := parseCloudWatchTimestamp("1705315845123")
		assert.True(t, ok)
		assert.False(t, ts.IsZero())
	})

	t.Run("Returns false for invalid format", func(t *testing.T) {
		_, ok := parseCloudWatchTimestamp("not-a-timestamp")
		assert.False(t, ok)
	})
}
