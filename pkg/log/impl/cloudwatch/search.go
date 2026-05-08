package cloudwatch

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// LogSearchResult implements LogSearchResult for CloudWatch.
type LogSearchResult struct {
	client  CWClient
	queryID string
	search  *client.LogSearch
	logger  *slog.Logger

	// cached results
	entries []client.LogEntry
	fields  ty.UniSet[string]
}

// GetSearch returns the search configuration.
func (r *LogSearchResult) GetSearch() *client.LogSearch {
	return r.search
}

// GetEntries polls for the query results and converts them.
func (r *LogSearchResult) fetchEntries(ctx context.Context) error {
	if len(r.entries) > 0 { // already fetched
		return nil
	}
	var results *cloudwatchlogs.GetQueryResultsOutput
	// Determine base polling interval from options; default 1s. Allow override via option: cloudwatchPollInterval (duration string)
	baseInterval := time.Second
	if ivStr, ok := r.search.Options.GetStringOk("cloudwatchPollInterval"); ok && ivStr != "" {
		if d, err := time.ParseDuration(ivStr); err == nil && d > 0 {
			baseInterval = d
		}
	}
	// Optional max interval (cap) for backoff
	maxInterval := 10 * time.Second
	if mxStr, ok := r.search.Options.GetStringOk("cloudwatchMaxPollInterval"); ok && mxStr != "" {
		if d, err := time.ParseDuration(mxStr); err == nil && d >= baseInterval {
			maxInterval = d
		}
	}
	// Backoff factor (default 1.5)
	backoffFactor := 1.5
	if bfStr, ok := r.search.Options.GetStringOk("cloudwatchPollBackoff"); ok && bfStr != "" {
		if f, err := strconv.ParseFloat(bfStr, 64); err == nil && f >= 1.0 && f <= 5.0 {
			backoffFactor = f
		}
	}
	interval := baseInterval
	for attempt := 0; ; attempt++ {
		var err error
		results, err = r.client.GetQueryResults(ctx, &cloudwatchlogs.GetQueryResultsInput{QueryId: &r.queryID})
		if err != nil {
			return err
		}
		if results.Status == types.QueryStatusComplete || results.Status == types.QueryStatusFailed || results.Status == types.QueryStatusCancelled {
			break
		}
		select {
		case <-time.After(interval):
			// Increase interval with backoff (exponential) until max
			interval = time.Duration(math.Min(float64(maxInterval), float64(interval)*backoffFactor))
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, resultFields := range results.Results {
		entry := client.LogEntry{Fields: make(ty.MI)}
		for _, field := range resultFields {
			if field.Field == nil || field.Value == nil {
				continue
			}
			fName := *field.Field
			fVal := *field.Value
			switch fName {
			case "@timestamp":
				if ts, ok := parseCloudWatchTimestamp(fVal); ok {
					entry.Timestamp = ts
				} else if r.logger != nil {
					r.logger.Warn("cloudwatch: failed to parse timestamp", "value", fVal)
				}
			case "@message":
				entry.Message = fVal
			default:
				entry.Fields[fName] = fVal
			}
		}
		r.entries = append(r.entries, entry)
	}
	return nil
}

// Err returns an error channel (unused for CloudWatch).
func (r *LogSearchResult) Err() <-chan error {
	return nil
}

// GetEntries returns log entries and a channel for streaming updates.
func (r *LogSearchResult) GetEntries(ctx context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	if err := r.fetchEntries(ctx); err != nil {
		return nil, nil, err
	}
	return r.entries, nil, nil
}

// GetFields retrieves distinct values for the specified fields.
func (r *LogSearchResult) GetFields(ctx context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	// If already computed, return cached
	if len(r.fields) > 0 {
		return r.fields, nil, nil
	}
	// Ensure entries are loaded with passed context for proper cancellation.
	if len(r.entries) == 0 {
		if err := r.fetchEntries(ctx); err != nil {
			return nil, nil, err
		}
	}
	fields := ty.UniSet[string]{}
	for _, e := range r.entries {
		for k, v := range e.Fields {
			if k == "@message" || k == "@timestamp" || k == "@ptr" || k == "@logStream" || k == "@log" || (len(k) > 0 && k[0] == '@') {
				continue
			}
			ty.AddField(k, v, &fields)
		}
	}
	r.fields = fields
	return fields, nil, nil
}

// parseCloudWatchTimestamp attempts to parse a CloudWatch Logs Insights timestamp.
// Common formats observed:
//   - "2006-01-02 15:04:05.000" (default in Insights results)
//   - time.RFC3339 or RFC3339Nano (defensive)
//   - Milliseconds since epoch (string of digits)
func parseCloudWatchTimestamp(v string) (time.Time, bool) {
	// Primary formats used in typical Insights outputs.
	layouts := []string{"2006-01-02 15:04:05.000", time.RFC3339Nano, time.RFC3339}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, v); err == nil {
			return ts, true
		}
	}
	// Try epoch millis (string of digits)
	if len(v) >= 13 { // at least millisecond precision length
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Unix(0, ms*int64(time.Millisecond)), true
		}
	}
	return time.Time{}, false
}

// GetPaginationInfo returns information for fetching the next page.
func (r *LogSearchResult) GetPaginationInfo() *client.PaginationInfo {
	if !r.search.Size.Set {
		return nil
	}

	// This method is called after GetEntries, which calls fetchEntries.
	// So r.entries should be populated.
	numResults := len(r.entries)
	if numResults < r.search.Size.Value {
		return nil // Last page
	}

	// The last entry's timestamp is the token for the next page.
	// Results are sorted desc, so the last entry is the oldest.
	lastEntry := r.entries[numResults-1]

	// Use RFC3339Nano for precision to avoid issues with duplicate timestamps as much as possible.
	nextPageToken := lastEntry.Timestamp.Format(time.RFC3339Nano)

	return &client.PaginationInfo{
		HasMore:       true,
		NextPageToken: nextPageToken,
	}
}
