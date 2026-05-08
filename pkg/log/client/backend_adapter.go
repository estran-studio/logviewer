package client

import (
	"context"

	"github.com/estran-studio/logviewer/pkg/ty"
)

// BackendAdapter adapts a LogBackend to the LogClient interface.
// It effectively "flattens" the streaming/async nature of LogBackend into
// synchronous, simple method calls for use cases that don't need streaming.
type BackendAdapter struct {
	Backend LogBackend
}

// NewBackendAdapter creates a new adapter for the given backend.
func NewBackendAdapter(backend LogBackend) *BackendAdapter {
	return &BackendAdapter{Backend: backend}
}

// Query executes a search and collects all results into a slice.
// It handles both synchronous (initial entries) and asynchronous (channel) results
// from the backend, blocking until all results are received.
func (a *BackendAdapter) Query(ctx context.Context, search LogSearch) ([]LogEntry, error) {
	result, err := a.Backend.Get(ctx, &search)
	if err != nil {
		return nil, err
	}

	entries, ch, err := result.GetEntries(ctx)
	if err != nil {
		return nil, err
	}

	// If a channel is returned, consume it until closed
	if ch != nil {
		for batch := range ch {
			entries = append(entries, batch...)
		}
	}

	return entries, nil
}

// GetFields returns available fields for the given search context.
func (a *BackendAdapter) GetFields(ctx context.Context, search LogSearch) (map[string][]string, error) {
	// Some backends might support GetFields directly via GetFieldValues or similar,
	// but the LogBackend interface exposes GetFields via the LogSearchResult.
	// We need to execute a search to get the result object, then call GetFields on it.

	// Optimization: If the backend has a dedicated method, we might use it, but
	// LogBackend doesn't have a direct GetFields. It has GetFieldValues.

	// Default approach: Execute a minimal search (size=1) to get the result handle?
	// Or just use the standard Get() with the provided search.
	// Note: Providing a full search might be expensive if we just want fields.
	// Ideally, the caller provides a search optimized for field discovery (e.g. limit=1).

	result, err := a.Backend.Get(ctx, &search)
	if err != nil {
		return nil, err
	}

	fieldsSet, ch, err := result.GetFields(ctx)
	if err != nil {
		return nil, err
	}

	// Consume channel if present (merging results)
	finalSet := make(ty.UniSet[string])
	if fieldsSet != nil {
		for k, v := range fieldsSet {
			// Manually merge to handle uniqueness via UniSet.Add
			for _, item := range v {
				finalSet.Add(k, item)
			}
		}
	}

	if ch != nil {
		for batch := range ch {
			for k, v := range batch {
				for _, item := range v {
					finalSet.Add(k, item)
				}
			}
		}
	}

	// Return the map directly as Ty.UniSet is compatible with map[string][]string
	return finalSet, nil
}

// GetValues returns distinct values for the specified field.
func (a *BackendAdapter) GetValues(ctx context.Context, search LogSearch, field string) ([]string, error) {
	// Use the backend's GetFieldValues method which is optimized for this
	valuesMap, err := a.Backend.GetFieldValues(ctx, &search, []string{field})
	if err != nil {
		return nil, err
	}

	if values, ok := valuesMap[field]; ok {
		return values, nil
	}

	return []string{}, nil
}
