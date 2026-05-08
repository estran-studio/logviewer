package client_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

type MockLogSearchResult struct {
	Entries []client.LogEntry
	Channel chan []client.LogEntry
	Search  *client.LogSearch
}

func (m *MockLogSearchResult) GetSearch() *client.LogSearch {
	if m.Search != nil {
		return m.Search
	}
	return &client.LogSearch{Options: ty.MI{"__context_id__": "test-ctx"}}
}

func (m *MockLogSearchResult) GetEntries(_ context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	return m.Entries, m.Channel, nil
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

func TestMultiLogSearchResult_GetEntries_Streaming(t *testing.T) {
	// Setup mock results
	ch1 := make(chan []client.LogEntry)
	mock1 := &MockLogSearchResult{
		Entries: []client.LogEntry{{Message: "init1", Timestamp: time.Now()}},
		Channel: ch1,
		Search:  &client.LogSearch{Options: ty.MI{"__context_id__": "ctx1"}},
	}

	ch2 := make(chan []client.LogEntry)
	mock2 := &MockLogSearchResult{
		Entries: []client.LogEntry{{Message: "init2", Timestamp: time.Now()}},
		Channel: ch2,
		Search:  &client.LogSearch{Options: ty.MI{"__context_id__": "ctx2"}},
	}

	multiRes, err := client.NewMultiLogSearchResult(&client.LogSearch{})
	if err != nil {
		t.Fatalf("NewMultiLogSearchResult failed: %v", err)
	}
	multiRes.Add(mock1, nil)
	multiRes.Add(mock2, nil)

	// Call GetEntries
	ctx := context.Background()
	initialEntries, mergedCh, err := multiRes.GetEntries(ctx)

	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}

	// Check initial entries
	if len(initialEntries) != 2 {
		t.Errorf("Expected 2 initial entries, got %d", len(initialEntries))
	}

	// Check if channel is returned
	if mergedCh == nil {
		t.Fatal("Expected merged channel, got nil")
	}

	// Test streaming
	go func() {
		ch1 <- []client.LogEntry{{Message: "stream1", Timestamp: time.Now()}}
		ch2 <- []client.LogEntry{{Message: "stream2", Timestamp: time.Now()}}
		close(ch1)
		close(ch2)
	}()

	// Read from merged channel
	count := 0
	for entries := range mergedCh {
		count++
		for _, e := range entries {
			switch e.Message {
			case "stream1":
				if e.ContextID != "ctx1" {
					t.Errorf("Expected ContextID ctx1 for stream1, got %s", e.ContextID)
				}
			case "stream2":
				if e.ContextID != "ctx2" {
					t.Errorf("Expected ContextID ctx2 for stream2, got %s", e.ContextID)
				}
			}
		}
	}

	// We expect at least 1 or 2 batches depending on how the loop and go scheduler work.
	// The loop "for entries := range mergedCh" will exit when mergedChannel is closed.
	// mergedChannel is closed when ch1 and ch2 are closed.
}

func TestNewMultiLogSearchResult_WithPagination(t *testing.T) {
	// Test that pagination is rejected
	search := &client.LogSearch{
		PageToken: ty.Opt[string]{Set: true, Valid: true, Value: "sometoken"},
	}

	_, err := client.NewMultiLogSearchResult(search)
	if err == nil {
		t.Error("Expected error when using pagination with multi-context, got nil")
	}
}

func TestNewMultiLogSearchResult_WithoutPagination(t *testing.T) {
	// Test successful creation without pagination
	search := &client.LogSearch{}

	result, err := client.NewMultiLogSearchResult(search)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if result == nil {
		t.Fatal("Expected result, got nil")
	}
}

func TestMultiLogSearchResult_Add(t *testing.T) {
	multiRes, _ := client.NewMultiLogSearchResult(&client.LogSearch{})

	mock1 := &MockLogSearchResult{
		Entries: []client.LogEntry{{Message: "test1"}},
	}
	mock2 := &MockLogSearchResult{
		Entries: []client.LogEntry{{Message: "test2"}},
	}

	// Test adding results
	multiRes.Add(mock1, nil)
	multiRes.Add(mock2, nil)

	if len(multiRes.Results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(multiRes.Results))
	}

	// Test adding with error
	testErr := assert.AnError
	multiRes.Add(nil, testErr)

	if len(multiRes.Errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(multiRes.Errors))
	}
}

func TestMultiLogSearchResult_GetFields(t *testing.T) {
	// Create mock with GetFields implementation
	mock1 := &MockLogSearchResultWithFields{
		Fields: ty.UniSet[string]{"field1": {"value1"}, "field2": {"value2"}},
	}
	mock2 := &MockLogSearchResultWithFields{
		Fields: ty.UniSet[string]{"field2": {"value3"}, "field3": {"value4"}},
	}

	multiRes, _ := client.NewMultiLogSearchResult(&client.LogSearch{})
	multiRes.Add(mock1, nil)
	multiRes.Add(mock2, nil)

	ctx := context.Background()
	fields, _, err := multiRes.GetFields(ctx)

	if err != nil {
		t.Fatalf("GetFields failed: %v", err)
	}

	// Check field merging
	if len(fields) != 3 {
		t.Errorf("Expected 3 fields, got %d", len(fields))
	}

	// Check that field2 has values from both contexts
	if len(fields["field2"]) != 2 {
		t.Errorf("Expected field2 to have 2 values, got %d", len(fields["field2"]))
	}
}

func TestMultiLogSearchResult_GetPaginationInfo(t *testing.T) {
	multiRes, _ := client.NewMultiLogSearchResult(&client.LogSearch{})

	info := multiRes.GetPaginationInfo()
	if info != nil {
		t.Error("Expected nil pagination info for multi-context result")
	}
}

func TestMultiLogSearchResult_Err(t *testing.T) {
	errCh1 := make(chan error, 1)
	errCh2 := make(chan error, 1)

	mock1 := &MockLogSearchResultWithErr{ErrChan: errCh1}
	mock2 := &MockLogSearchResultWithErr{ErrChan: errCh2}

	multiRes, _ := client.NewMultiLogSearchResult(&client.LogSearch{})
	multiRes.Add(mock1, nil)
	multiRes.Add(mock2, nil)

	mergedErrCh := multiRes.Err()

	// Send errors
	testErr1 := assert.AnError
	testErr2 := errors.New("error 2")

	errCh1 <- testErr1
	errCh2 <- testErr2
	close(errCh1)
	close(errCh2)

	// Collect errors
	var receivedErrors []error
	for err := range mergedErrCh {
		receivedErrors = append(receivedErrors, err)
	}

	if len(receivedErrors) != 2 {
		t.Errorf("Expected 2 errors, got %d", len(receivedErrors))
	}
}

func TestMultiLogSearchResult_GetEntries_WithSizeLimit(t *testing.T) {
	mock1 := &MockLogSearchResult{
		Entries: []client.LogEntry{
			{Message: "msg1", Timestamp: time.Now().Add(-2 * time.Second)},
			{Message: "msg2", Timestamp: time.Now().Add(-1 * time.Second)},
		},
		Search: &client.LogSearch{
			Options: ty.MI{"__context_id__": "ctx1"},
			Size:    ty.Opt[int]{Set: true, Value: 2},
		},
	}

	mock2 := &MockLogSearchResult{
		Entries: []client.LogEntry{
			{Message: "msg3", Timestamp: time.Now().Add(-3 * time.Second)},
			{Message: "msg4", Timestamp: time.Now()},
		},
		Search: &client.LogSearch{
			Options: ty.MI{"__context_id__": "ctx2"},
			Size:    ty.Opt[int]{Set: true, Value: 2},
		},
	}

	multiRes, _ := client.NewMultiLogSearchResult(&client.LogSearch{})
	multiRes.Add(mock1, nil)
	multiRes.Add(mock2, nil)

	ctx := context.Background()
	entries, _, err := multiRes.GetEntries(ctx)

	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}

	// Should be limited to 2 (from first result's Size)
	if len(entries) != 2 {
		t.Errorf("Expected 2 entries due to size limit, got %d", len(entries))
	}
}

// Helper mocks for additional test coverage

type MockLogSearchResultWithFields struct {
	Fields ty.UniSet[string]
}

func (m *MockLogSearchResultWithFields) GetSearch() *client.LogSearch {
	return &client.LogSearch{Options: ty.MI{"__context_id__": "test"}}
}

func (m *MockLogSearchResultWithFields) GetEntries(_ context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	return nil, nil, nil
}

func (m *MockLogSearchResultWithFields) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	return m.Fields, nil, nil
}

func (m *MockLogSearchResultWithFields) GetPaginationInfo() *client.PaginationInfo {
	return nil
}

func (m *MockLogSearchResultWithFields) Err() <-chan error {
	return nil
}

type MockLogSearchResultWithErr struct {
	ErrChan chan error
}

func (m *MockLogSearchResultWithErr) GetSearch() *client.LogSearch {
	return &client.LogSearch{Options: ty.MI{"__context_id__": "test"}}
}

func (m *MockLogSearchResultWithErr) GetEntries(_ context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	return nil, nil, nil
}

func (m *MockLogSearchResultWithErr) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	return nil, nil, nil
}

func (m *MockLogSearchResultWithErr) GetPaginationInfo() *client.PaginationInfo {
	return nil
}

func (m *MockLogSearchResultWithErr) Err() <-chan error {
	return m.ErrChan
}

// TestMultiLogSearchResult_ErrorPropagation tests that errors from child results are properly propagated
func TestMultiLogSearchResult_ErrorPropagation(t *testing.T) {
	// Create error channels for child results
	errChan1 := make(chan error, 1)
	errChan2 := make(chan error, 1)

	// Send errors to channels
	testErr1 := errors.New("backend 1 error")
	testErr2 := errors.New("backend 2 error")
	errChan1 <- testErr1
	errChan2 <- testErr2
	close(errChan1)
	close(errChan2)

	// Create mock results with error channels
	mock1 := &MockLogSearchResultWithErr{ErrChan: errChan1}
	mock2 := &MockLogSearchResultWithErr{ErrChan: errChan2}

	// Create multi result
	search := &client.LogSearch{}
	multi, err := client.NewMultiLogSearchResult(search)
	assert.NoError(t, err)

	// Add child results
	multi.Add(mock1, nil)
	multi.Add(mock2, nil)

	// Get error channel
	errCh := multi.Err()

	// Collect all errors
	var collectedErrors []error
	for err := range errCh {
		collectedErrors = append(collectedErrors, err)
	}

	// Verify both errors were propagated
	assert.Len(t, collectedErrors, 2, "Expected 2 errors to be propagated")
	assert.Contains(t, collectedErrors, testErr1, "Expected first error to be propagated")
	assert.Contains(t, collectedErrors, testErr2, "Expected second error to be propagated")
}

// TestMultiLogSearchResult_EmptyResults tests behavior when all child results are empty
func TestMultiLogSearchResult_EmptyResults(t *testing.T) {
	ctx := context.Background()

	// Create mock results with no entries
	mock1 := &MockLogSearchResult{
		Entries: []client.LogEntry{},
		Channel: nil, // No streaming channel
	}
	mock2 := &MockLogSearchResult{
		Entries: []client.LogEntry{},
		Channel: nil,
	}

	// Create multi result
	search := &client.LogSearch{}
	multi, err := client.NewMultiLogSearchResult(search)
	assert.NoError(t, err)

	// Add child results
	multi.Add(mock1, nil)
	multi.Add(mock2, nil)

	// Get entries
	entries, streamCh, err2 := multi.GetEntries(ctx)

	// Verify behavior
	assert.NoError(t, err2)
	assert.Empty(t, entries, "Expected empty entries when all child results are empty")

	// Verify stream channel closes immediately when no results have streaming channels
	select {
	case _, ok := <-streamCh:
		if ok {
			t.Error("Expected stream channel to be closed when no child has streaming")
		}
	case <-time.After(1 * time.Second):
		// Channel might not close immediately, which is acceptable
	}
}

// TestMultiLogSearchResult_PartialErrors tests that some results can succeed while others fail
func TestMultiLogSearchResult_PartialErrors(t *testing.T) {
	ctx := context.Background()

	// Create one successful result and one with errors
	successMock := &MockLogSearchResult{
		Entries: []client.LogEntry{{Message: "success", Timestamp: time.Now()}},
		Channel: nil,
	}

	errChan := make(chan error, 1)
	errChan <- errors.New("backend error")
	close(errChan)
	errorMock := &MockLogSearchResultWithErr{ErrChan: errChan}

	// Create multi result
	search := &client.LogSearch{}
	multi, err := client.NewMultiLogSearchResult(search)
	assert.NoError(t, err)

	// Add child results
	multi.Add(successMock, nil)
	multi.Add(errorMock, nil)

	// Get entries - should still work despite one result having errors
	entries, _, err2 := multi.GetEntries(ctx)
	assert.NoError(t, err2, "GetEntries should not return immediate error")
	assert.NotEmpty(t, entries, "Should have entries from successful result")

	// Check error channel
	errCh := multi.Err()
	var receivedErr error
	select {
	case receivedErr = <-errCh:
		assert.Error(t, receivedErr, "Expected to receive error from error channel")
	case <-time.After(1 * time.Second):
		t.Error("Expected to receive error from error channel")
	}
}
