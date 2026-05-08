package logclient

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/impl/splunk/restapi"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// SplunkLogSearchResult implements LogSearchResult for Splunk.
type SplunkLogSearchResult struct {
	logClient *SplunkLogSearchClient
	sid       string
	search    *client.LogSearch
	isFollow  bool

	results []restapi.SearchResultsResponse

	// parsed offset from the incoming page token (set by client.Get)
	CurrentOffset int
	// useResultsEndpoint indicates if the query has transforming commands
	// (stats, chart, etc.) requiring the /results endpoint instead of /events
	useResultsEndpoint bool
	// sizeLimit enforces max number of entries to return (0 = no limit)
	sizeLimit int
}

// GetSearch returns the search configuration.
func (s SplunkLogSearchResult) GetSearch() *client.LogSearch {
	return s.search
}

// Close cancels the running Splunk search job.
func (s *SplunkLogSearchResult) Close() error {
	if s.isFollow {
		log.Printf("closing splunk search job %s", s.sid)
		return s.logClient.client.CancelSearchJob(s.sid)
	}
	return nil
}

// GetEntries returns log entries and a channel for streaming updates.
func (s SplunkLogSearchResult) GetEntries(ctx context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	if !s.isFollow {
		entries := s.parseResults(&s.results[0])
		// Apply size limit if set
		if s.sizeLimit > 0 && len(entries) > s.sizeLimit {
			entries = entries[:s.sizeLimit]
		}
		return entries, nil, nil
	}

	entryChan := make(chan []client.LogEntry)
	go func() {
		defer close(entryChan)
		offset := 0
		// set polling interval
		pollInterval := 2 * time.Second
		if s.logClient.options.FollowPollIntervalSeconds > 0 {
			pollInterval = time.Duration(s.logClient.options.FollowPollIntervalSeconds) * time.Second
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollInterval):
				log.Printf("polling for new events for job %s", s.sid)
				// for a follow, we get all the events every time
				results, err := s.logClient.client.GetSearchResult(s.sid, offset, 0, s.useResultsEndpoint)
				if err != nil {
					log.Printf("error while polling for events: %s", err)
					continue
				}

				if len(results.Results) > 0 {
					entries := s.parseResults(&results)
					// Apply size limit for follow mode
					if s.sizeLimit > 0 && offset+len(entries) > s.sizeLimit {
						remaining := s.sizeLimit - offset
						if remaining > 0 {
							entries = entries[:remaining]
							entryChan <- entries
							offset += len(entries)
						}
						return
					}
					entryChan <- entries
					offset += len(entries)
				}
			}
		}
	}()

	return nil, entryChan, nil
}

// GetFields extracts the set of unique field names from the search results.
func (s SplunkLogSearchResult) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	fields := ty.UniSet[string]{}

	for _, resultEntry := range s.results {
		for _, result := range resultEntry.Results {
			for k, v := range result {
				if k[0] == '_' {
					continue
				}

				ty.AddField(k, v, &fields)
			}
		}
	}

	return fields, nil, nil
}

// GetPaginationInfo returns information for fetching the next page.
func (s SplunkLogSearchResult) GetPaginationInfo() *client.PaginationInfo {
	if s.isFollow || !s.search.Size.Set {
		return nil
	}

	// Use the offset parsed and stored by the client.Get implementation. If the
	// result was constructed manually (e.g. in tests) the default is 0 which
	// preserves previous behavior.
	currentOffset := s.CurrentOffset

	numResults := len(s.results[0].Results)

	// If we got fewer results than requested, this is the last page
	if numResults < s.search.Size.Value {
		return nil
	}

	return &client.PaginationInfo{
		HasMore:       true,
		NextPageToken: strconv.Itoa(currentOffset + numResults),
	}
}

func (s SplunkLogSearchResult) parseResults(searchResponse *restapi.SearchResultsResponse) []client.LogEntry {

	entries := make([]client.LogEntry, len(searchResponse.Results))

	for i, result := range searchResponse.Results {
		var timestamp time.Time
		var message string

		// For transforming commands (stats, chart, etc.), results don't have _raw
		// but may have _time (e.g., timechart) or _span
		if s.useResultsEndpoint {
			// Try to parse _time if present (timechart, chart results have it)
			// If missing or unparseable, timestamp remains zero-value to indicate unknown
			if timeStr := result.GetString("_time"); timeStr != "" {
				var err error
				timestamp, err = time.Parse(time.RFC3339, timeStr)
				if err != nil {
					// Try alternative format used by Splunk
					timestamp, _ = time.Parse("2006-01-02T15:04:05.000-07:00", timeStr)
					// If still fails, timestamp remains zero-value (time.Time{})
				}
			}
			// Build message from all fields for display
			message = s.formatAggregatedResult(result)
		} else {
			// Standard event parsing
			var err error
			timestamp, err = time.Parse(time.RFC3339, result.GetString("_time"))
			if err != nil {
				log.Println("warning failed to parsed timestamp " + result.GetString("_time"))
			}
			message = result.GetString("_raw")
		}

		entries[i].Message = message
		entries[i].Timestamp = timestamp
		entries[i].Level = ""
		entries[i].Fields = ty.MI{}

		for k, v := range result {
			if k[0] != '_' {
				entries[i].Fields[k] = v
			}
		}
	}

	// Sort entries by timestamp in ascending order (oldest first)
	// This ensures consistent ordering with multi-context queries
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	return entries

}

// formatAggregatedResult formats a stats/aggregated result as a readable string
func (s SplunkLogSearchResult) formatAggregatedResult(result ty.MI) string {
	// Collect keys and sort for consistent output
	var keys []string
	for k := range result {
		if k[0] != '_' {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, result[k]))
	}
	return strings.Join(parts, "  ")
}

// Err returns an error channel (unused for Splunk).
func (s SplunkLogSearchResult) Err() <-chan error {
	return nil
}
