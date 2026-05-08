package client

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/estran-studio/logviewer/pkg/ty"
)

// MultiLogSearchResult aggregates multiple LogSearchResult objects into a single,
// unified result. It is designed for multi-context queries where logs from
// different sources need to be merged and presented as a single stream.
type MultiLogSearchResult struct {
	// a slice of the individual LogSearchResult objects from each queried context.
	Results []LogSearchResult
	// a slice of errors encountered during the concurrent query execution.
	Errors []error
	// the original LogSearch request that initiated the multi-context query.
	Search *LogSearch
	// mutex to protect concurrent access to Results and Errors slices.
	mutex sync.Mutex
}

// ensure MultiLogSearchResult implements the LogSearchResult interface.
var _ LogSearchResult = (*MultiLogSearchResult)(nil)

// NewMultiLogSearchResult creates and returns a new MultiLogSearchResult.
// It validates that the search request doesn't contain unsupported features for multi-context queries.
func NewMultiLogSearchResult(search *LogSearch) (*MultiLogSearchResult, error) {
	// Validate that unsupported features are not used
	if search.PageToken.Set && search.PageToken.Valid {
		return nil, errors.New("pagination is not supported with multiple contexts; use a single context instead")
	}

	return &MultiLogSearchResult{
		Search:  search,
		Results: []LogSearchResult{},
		Errors:  []error{},
	}, nil
}

// Add appends a search result and an associated error to the aggregator.
// This method is safe for concurrent use.
func (m *MultiLogSearchResult) Add(result LogSearchResult, err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if err != nil {
		m.Errors = append(m.Errors, err)
	}
	if result != nil {
		m.Results = append(m.Results, result)
	}
}

// GetSearch returns the original LogSearch request.
func (m *MultiLogSearchResult) GetSearch() *LogSearch {
	return m.Search
}

// GetEntries merges log entries from all successful search results, sorts them
// by timestamp, and returns them. It also populates the ContextID for each entry.
func (m *MultiLogSearchResult) GetEntries(ctx context.Context) ([]LogEntry, chan []LogEntry, error) {
	var allEntries []LogEntry
	var mutex sync.Mutex
	var wg sync.WaitGroup
	var subChannels []chan []LogEntry
	var subChannelsResults []LogSearchResult

	for _, result := range m.Results {
		wg.Add(1)
		go func(r LogSearchResult) {
			defer wg.Done()
			entries, ch, err := r.GetEntries(ctx)
			if err != nil {
				// In a real-world scenario, you might want to handle this error more gracefully.
				// For now, we'll just skip the results from this source.
				return
			}

			// Get the search config for this individual result
			resultSearch := r.GetSearch()

			// Populate ContextID and apply JSON extraction for each entry
			contextID, ok := resultSearch.Options["__context_id__"].(string)
			if !ok {
				contextID = "unknown"
			}
			for i := range entries {
				entries[i].ContextID = contextID
				// Apply JSON extraction based on this result's search config
				ExtractJSONFromEntry(&entries[i], resultSearch)
			}

			mutex.Lock()
			allEntries = append(allEntries, entries...)
			if ch != nil {
				subChannels = append(subChannels, ch)
				subChannelsResults = append(subChannelsResults, r)
			}
			mutex.Unlock()
		}(result)
	}

	wg.Wait()

	// Sort the combined slice of log entries by timestamp.
	sort.SliceStable(allEntries, func(i, j int) bool {
		return allEntries[i].Timestamp.Before(allEntries[j].Timestamp)
	})

	// Apply global size limit if specified in the search
	globalSizeLimit := 0
	if len(m.Results) > 0 {
		firstSearch := m.Results[0].GetSearch()
		if firstSearch.Size.Set && firstSearch.Size.Value > 0 {
			globalSizeLimit = firstSearch.Size.Value
		}
	}

	// Truncate to global size limit if needed
	if globalSizeLimit > 0 && len(allEntries) > globalSizeLimit {
		allEntries = allEntries[:globalSizeLimit]
	}

	var mergedChannel chan []LogEntry
	if len(subChannels) > 0 {
		mergedChannel = make(chan []LogEntry)

		go func() {
			var wgCh sync.WaitGroup
			for i, ch := range subChannels {
				wgCh.Add(1)
				go func(c chan []LogEntry, r LogSearchResult) {
					defer wgCh.Done()
					for entries := range c {
						resultSearch := r.GetSearch()
						contextID, ok := resultSearch.Options["__context_id__"].(string)
						if !ok {
							contextID = "unknown"
						}
						for k := range entries {
							entries[k].ContextID = contextID
							ExtractJSONFromEntry(&entries[k], resultSearch)
						}
						mergedChannel <- entries
					}
				}(ch, subChannelsResults[i])
			}
			wgCh.Wait()
			close(mergedChannel)
		}()
	}

	return allEntries, mergedChannel, nil
}

// GetFields merges fields from all contexts in the result.
func (m *MultiLogSearchResult) GetFields(ctx context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	aggregatedFields := make(ty.UniSet[string])
	var hasError bool

	for _, res := range m.Results {
		fields, _, err := res.GetFields(ctx)
		if err != nil {
			m.Errors = append(m.Errors, err)
			hasError = true
			continue
		}

		if fields != nil {
			for k, values := range fields {
				for _, v := range values {
					aggregatedFields.Add(k, v)
				}
			}
		}
	}

	if hasError && len(aggregatedFields) == 0 {
		return nil, nil, errors.New("failed to get fields from any context")
	}

	return aggregatedFields, nil, nil
}

// GetPaginationInfo returns nil as pagination is not supported for multi-context search results.
func (m *MultiLogSearchResult) GetPaginationInfo() *PaginationInfo {
	// Pagination is not supported for merged results.
	return nil
}

// Err merges the error channels from all underlying LogSearchResult objects.
// It returns a new channel that will receive errors from any of the individual
// search results. The returned channel is closed once all underlying error
// channels are closed.
func (m *MultiLogSearchResult) Err() <-chan error {
	var wg sync.WaitGroup
	// a buffer size equal to the number of results
	mergedErrChan := make(chan error, len(m.Results))

	for _, r := range m.Results {
		if r.Err() == nil {
			continue
		}
		wg.Add(1)
		go func(errChan <-chan error) {
			defer wg.Done()
			for err := range errChan {
				mergedErrChan <- err
			}
		}(r.Err())
	}

	go func() {
		wg.Wait()
		close(mergedErrChan)
	}()

	return mergedErrChan
}
