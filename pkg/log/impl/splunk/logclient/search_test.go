package logclient

import (
	"context"
	"testing"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/impl/splunk/restapi"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/h2non/gock"
	"github.com/stretchr/testify/assert"
)

func TestSplunkLogSearchResult_GetEntries_Follow(t *testing.T) {
	defer gock.Off()

	sid := "my-follow-sid"
	gock.New("http://splunk.com:8080").
		Get("/search/jobs/" + sid + "/events").
		Reply(200).
		JSON(ty.MI{
			"results": []ty.MS{
				{"_raw": "new log 1"},
			},
		})

	logClient, err := GetClient(SplunkLogSearchClientOptions{
		URL:                       "http://splunk.com:8080",
		FollowPollIntervalSeconds: 1,
	})
	assert.NoError(t, err)

	splunkClient := logClient.(SplunkLogSearchClient)
	searchResult := SplunkLogSearchResult{
		logClient: &splunkClient,
		sid:       sid,
		isFollow:  true,
		search:    &client.LogSearch{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, entryChan, err := searchResult.GetEntries(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, entryChan)

	select {
	case entries, ok := <-entryChan:
		assert.True(t, ok)
		assert.Len(t, entries, 1)
		assert.Equal(t, "new log 1", entries[0].Message)
	case <-ctx.Done():
		t.Fatal("timed out waiting for log entries")
	}

	assert.True(t, gock.IsDone())
}

func TestSplunkLogSearchResult_Close(t *testing.T) {
	defer gock.Off()

	sid := "my-follow-sid"
	gock.New("http://splunk.com:8080").
		Delete("/search/jobs/" + sid).
		Reply(200)

	logClient, err := GetClient(SplunkLogSearchClientOptions{
		URL: "http://splunk.com:8080",
	})
	assert.NoError(t, err)

	splunkClient := logClient.(SplunkLogSearchClient)
	searchResult := SplunkLogSearchResult{
		logClient: &splunkClient,
		sid:       sid,
		isFollow:  true,
		search:    &client.LogSearch{},
	}

	err = searchResult.Close()
	assert.NoError(t, err)

	assert.True(t, gock.IsDone())
}

func TestSplunkLogSearchResult_parseResults_SortsAscending(t *testing.T) {
	// Test that parseResults sorts entries by timestamp in ascending order (oldest first)
	searchResult := SplunkLogSearchResult{
		useResultsEndpoint: false,
	}

	// Create search response with timestamps in descending order (newest first - Splunk default)
	searchResponse := &restapi.SearchResultsResponse{
		Results: []ty.MI{
			{
				"_time": "2026-01-06T11:11:45.714-08:00",
				"_raw":  "newest log entry",
			},
			{
				"_time": "2026-01-06T11:11:45.711-08:00",
				"_raw":  "middle log entry",
			},
			{
				"_time": "2026-01-06T11:11:34.209-08:00",
				"_raw":  "oldest log entry",
			},
		},
	}

	entries := searchResult.parseResults(searchResponse)

	// Verify entries are sorted oldest first
	assert.Len(t, entries, 3)
	assert.Equal(t, "oldest log entry", entries[0].Message)
	assert.Equal(t, "middle log entry", entries[1].Message)
	assert.Equal(t, "newest log entry", entries[2].Message)

	// Verify timestamps are in ascending order
	assert.True(t, entries[0].Timestamp.Before(entries[1].Timestamp))
	assert.True(t, entries[1].Timestamp.Before(entries[2].Timestamp))
}

func TestSplunkLogSearchResult_parseResults_StandardEvents(t *testing.T) {
	// Test standard event parsing (useResultsEndpoint = false)
	searchResult := SplunkLogSearchResult{
		useResultsEndpoint: false,
	}

	searchResponse := &restapi.SearchResultsResponse{
		Results: []ty.MI{
			{
				"_time":            "2026-01-06T11:11:45.714-08:00",
				"_raw":             "application log message here",
				"application_name": "checkout",
				"level":            "ERROR",
			},
		},
	}

	entries := searchResult.parseResults(searchResponse)

	assert.Len(t, entries, 1)
	assert.Equal(t, "application log message here", entries[0].Message)
	assert.Equal(t, "checkout", entries[0].Fields["application_name"])
	assert.Equal(t, "ERROR", entries[0].Fields["level"])
	assert.NotContains(t, entries[0].Fields, "_raw")
	assert.NotContains(t, entries[0].Fields, "_time")
}

func TestSplunkLogSearchResult_parseResults_TransformingCommands(t *testing.T) {
	// Test transforming command results (useResultsEndpoint = true)
	searchResult := SplunkLogSearchResult{
		useResultsEndpoint: true,
	}

	searchResponse := &restapi.SearchResultsResponse{
		Results: []ty.MI{
			{
				"_time":  "2026-01-06T11:11:45.714-08:00",
				"count":  "150",
				"status": "500",
			},
		},
	}

	entries := searchResult.parseResults(searchResponse)

	assert.Len(t, entries, 1)
	// Message should be formatted as key=value pairs
	assert.Contains(t, entries[0].Message, "count=150")
	assert.Contains(t, entries[0].Message, "status=500")
	assert.Equal(t, "150", entries[0].Fields["count"])
	assert.Equal(t, "500", entries[0].Fields["status"])
}
