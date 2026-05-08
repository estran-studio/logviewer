// Package elk provides Elasticsearch-specific types and search result
// handling for log retrieval implementations.
package elk

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// Hit represents a single hit returned by Elasticsearch for a document.
type Hit struct {
	Index  string `json:"_index"`
	Type   string `json:"_type"`
	ID     string `json:"_id"`
	Score  int32  `json:"_score"`
	Source ty.MI  `json:"_source"`
}

// Hits is a wrapper for the hit list returned by an Elasticsearch query.
type Hits struct {
	// total
	// max_score
	Hits []Hit `json:"hits"`
}

// SearchResult implements client search results for Elasticsearch
// responses and provides convenience methods to extract entries and
// pagination information.
type SearchResult struct {
	client client.LogBackend
	search *client.LogSearch
	result Hits

	// store loaded entries

	// store extracted fields
	// parsed offset from the incoming page token (set by client.Get)
	CurrentOffset int
	ErrChan       chan error
}

// NewSearchResult constructs a SearchResult from a client, search
// description and raw hits returned by Elasticsearch.
func NewSearchResult(client client.LogBackend, search *client.LogSearch, hits Hits) SearchResult {
	return SearchResult{
		client:  client,
		search:  search,
		result:  hits,
		ErrChan: make(chan error, 1),
	}
}

// GetSearch returns the original LogSearch used to produce this result.
func (sr SearchResult) GetSearch() *client.LogSearch {
	return sr.search
}

// GetEntries returns the parsed log entries for this result, a channel
// that will receive updates when the search is refreshed, and any
// immediate error encountered during setup.
func (sr SearchResult) GetEntries(context context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {

	entries := sr.parseResults()

	c, err := sr.onChange(context)

	return entries, c, err
}

// GetFields extracts a set of field names and values present in the
// search results. It returns the set, an update channel (currently
// unused) and an error if one occurs.
func (sr SearchResult) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {

	fields := ty.UniSet[string]{}

	for _, h := range sr.result.Hits {
		for k, v := range h.Source {
			if k == "message" || k == "@timestamp" {
				continue
			}
			ty.AddField(k, v, &fields)
		}
	}
	return fields, nil, nil
}

func (sr SearchResult) parseResults() []client.LogEntry {
	size := len(sr.result.Hits)

	entries := make([]client.LogEntry, size)

	log.Printf("receive %d for %s"+ty.LB, len(entries), sr.search.Options.GetString("index"))

	for i, h := range sr.result.Hits {
		message, b := h.Source["message"].(string)
		if !b {
			fmt.Printf("message is not string : %+v \n", h.Source["message"])
			entries[size-i-1] = client.LogEntry{}
			continue
		}
		if timestamp, b1 := h.Source["@timestamp"].(string); b1 {
			// Try high precision first, then standard RFC3339
			var date time.Time
			var err error
			if date, err = time.Parse(time.RFC3339Nano, timestamp); err != nil {
				if date, err = time.Parse(ty.Format, timestamp); err != nil {
					// Fallback: leave zero-value time (or set to now)
					date = time.Time{}
				}
			}

			var level string
			if h.Source["level"] != nil {
				level, _ = h.Source["level"].(string)
			}

			entries[size-i-1] = client.LogEntry{
				Message:   message,
				Timestamp: date,
				Level:     level, Fields: h.Source}
		} else {
			fmt.Printf("timestamp is not string : %+v \n", h.Source["@timestamp"])
		}
	}

	return entries
}

// GetPaginationInfo returns pagination details (has more / next page
// token) when the search explicitly requested a size and more results
// are available.
func (sr SearchResult) GetPaginationInfo() *client.PaginationInfo {
	if !sr.search.Size.Set {
		return nil
	}

	// Use the offset parsed and stored by the client.Get implementation. If
	// the result was constructed manually (e.g. in tests) the default is 0.
	currentOffset := sr.CurrentOffset

	numResults := len(sr.result.Hits)

	// If we got fewer results than requested, this is the last page
	if numResults < sr.search.Size.Value {
		return nil
	}

	return &client.PaginationInfo{
		HasMore:       true,
		NextPageToken: strconv.Itoa(currentOffset + numResults),
	}
}

// Err returns a channel that will receive asynchronous errors
// produced while watching for search refreshes.
func (sr SearchResult) Err() <-chan error {
	return sr.ErrChan
}

func (sr SearchResult) onChange(ctx context.Context) (chan []client.LogEntry, error) {
	if sr.search.Refresh.Duration.Value == "" {
		return nil, nil
	}

	duration, err := time.ParseDuration(sr.search.Refresh.Duration.Value)
	if err != nil {
		return nil, err
	}

	c := make(chan []client.LogEntry, 5)
	go func() {
		// Initialize lastLte from current search or use time.Now() as fallback
		var lastLte time.Time
		var err error

		customFormat := sr.search.Options.GetString("timestampFormat")

		if sr.search.Range.Lte.Value != "" {
			if customFormat != "" {
				lastLte, err = time.Parse(customFormat, sr.search.Range.Lte.Value)
			} else {
				// Try parsing with nanoseconds first, then without.
				lastLte, err = time.Parse(time.RFC3339Nano, sr.search.Range.Lte.Value)
				if err != nil {
					lastLte, err = time.Parse(time.RFC3339, sr.search.Range.Lte.Value)
				}
			}

			if err != nil {
				lastLte = time.Now()
			}
		} else {
			lastLte = time.Now()
		}

		for {
			select {
			case <-time.After(duration):
				{
					format := time.RFC3339
					if customFormat != "" {
						format = customFormat
					}

					// Use the last Lte + 1 second as the new Gte (sliding window)
					sr.search.Range.Gte.Value = lastLte.Add(time.Second * 1).Format(format)
					newLte := time.Now()
					sr.search.Range.Lte.Value = newLte.Format(format)
					// Clear Last to avoid conflict with Gte/Lte
					sr.search.Range.Last.Value = ""
					sr.search.Range.Last.Set = false

					result, err := sr.client.Get(ctx, sr.search)
					if err != nil {
						sr.ErrChan <- fmt.Errorf("failed to get new logs: %w", err)
						continue
					}
					lastLte = newLte
					c <- result.(SearchResult).parseResults()
				}
			case <-ctx.Done():
				close(c)
				return
			}
		}
	}()
	return c, nil
}
