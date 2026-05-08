// Package logclient provides the Splunk implementation of the log client interface.
package logclient

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	httpPkg "github.com/estran-studio/logviewer/pkg/http"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/impl/splunk/restapi"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// Increase the retry limit because Splunk search jobs can take several seconds
// to dispatch on a fresh dev instance.
const maxRetryDoneJob = 30

// SplunkAuthOptions defines authentication headers.
type SplunkAuthOptions struct {
	Header ty.MS `json:"header" yaml:"header"`
}

// SplunkLogSearchClientOptions defines configuration for the Splunk client.
type SplunkLogSearchClientOptions struct {
	URL string `json:"url" yaml:"url"`

	Auth       SplunkAuthOptions `json:"auth" yaml:"auth"`
	Headers    ty.MS             `json:"headers" yaml:"headers"`
	SearchBody ty.MS             `json:"searchBody" yaml:"searchBody"`
	// Polling configuration
	PollIntervalSeconds       int `json:"pollIntervalSeconds" yaml:"pollIntervalSeconds"`
	FollowPollIntervalSeconds int `json:"followPollIntervalSeconds" yaml:"followPollIntervalSeconds"`
	MaxRetries                int `json:"maxRetries" yaml:"maxRetries"`
}

// SplunkLogSearchClient implements LogClient for Splunk.
type SplunkLogSearchClient struct {
	client restapi.SplunkRestClient

	options SplunkLogSearchClientOptions
}

// Get executes a search against Splunk.
func (s SplunkLogSearchClient) Get(ctx context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
	// initiate the things and wait for query to be done

	if s.options.Headers == nil {
		s.options.Headers = ty.MS{}
	}

	if s.options.SearchBody == nil {
		s.options.SearchBody = ty.MS{}
	}

	searchRequest, err := getSearchRequest(search)
	if err != nil {
		return nil, err
	}

	// Detect if query contains transforming commands (stats, chart, etc.)
	// These require fetching from /results endpoint instead of /events
	queryString := searchRequest["search"]
	useResultsEndpoint := ContainsTransformingCommand(queryString)

	searchJobResponse, err := s.client.CreateSearchJob(queryString, searchRequest["earliest_time"], searchRequest["latest_time"], search.Follow, s.options.Headers, s.options.SearchBody)
	if err != nil {
		return nil, err
	}

	if search.Follow {
		// Determine size limit for follow mode
		sizeLimit := 0
		if search.Size.Set && search.Size.Value > 0 {
			sizeLimit = search.Size.Value
		}
		return SplunkLogSearchResult{
			logClient:          &s,
			search:             search,
			sid:                searchJobResponse.Sid,
			isFollow:           true,
			useResultsEndpoint: useResultsEndpoint,
			sizeLimit:          sizeLimit,
		}, nil
	}

	// configure polling
	pollInterval := time.Duration(1) * time.Second
	maxRetries := maxRetryDoneJob
	if s.options.PollIntervalSeconds > 0 {
		pollInterval = time.Duration(s.options.PollIntervalSeconds) * time.Second
	}
	if s.options.MaxRetries > 0 {
		maxRetries = s.options.MaxRetries
	}

	isDone := false
	tryCount := 0

	// wait until job is done or retries exhausted
	for {
		if tryCount >= maxRetries {
			// When the job is done, we should cancel it
			defer func() { _ = s.client.CancelSearchJob(searchJobResponse.Sid) }()
			return nil, errors.New("number of retry for splunk job failed")
		}

		select {
		case <-ctx.Done():
			// When the job is done, we should cancel it
			defer func() { _ = s.client.CancelSearchJob(searchJobResponse.Sid) }()
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
		log.Printf("waiting for splunk job %s to complete (try %d/%d)", searchJobResponse.Sid, tryCount+1, maxRetries)

		status, err := s.client.GetSearchStatus(searchJobResponse.Sid)

		if err != nil {
			return nil, err
		}

		// Guard against responses with no entry
		if len(status.Entry) > 0 {
			isDone = status.Entry[0].Content.IsDone
		} else {
			isDone = false
		}

		if isDone {
			// When the job is done, we should cancel it
			defer func() { _ = s.client.CancelSearchJob(searchJobResponse.Sid) }()
			break
		}

		tryCount++
	}

	offset := 0
	if search.PageToken.Value != "" {
		var err error
		offset, err = strconv.Atoi(search.PageToken.Value)
		if err != nil {
			return nil, fmt.Errorf("invalid page token: %w", err)
		}
	}

	firstResult, err := s.client.GetSearchResult(searchJobResponse.Sid, offset, search.Size.Value, useResultsEndpoint)

	if err != nil {
		return nil, err
	}

	// Determine size limit for enforcing in GetEntries
	sizeLimit := 0
	if search.Size.Set && search.Size.Value > 0 {
		sizeLimit = search.Size.Value
	}

	return SplunkLogSearchResult{
		logClient:          &s,
		search:             search,
		sid:                searchJobResponse.Sid,
		results:            []restapi.SearchResultsResponse{firstResult},
		CurrentOffset:      offset,
		useResultsEndpoint: useResultsEndpoint,
		sizeLimit:          sizeLimit,
	}, nil
}

// GetFieldValues retrieves distinct values for the specified fields.
func (s SplunkLogSearchClient) GetFieldValues(ctx context.Context, search *client.LogSearch, fields []string) (map[string][]string, error) {
	if s.options.Headers == nil {
		s.options.Headers = ty.MS{}
	}
	if s.options.SearchBody == nil {
		s.options.SearchBody = ty.MS{}
	}

	// Build the base search request
	searchRequest, err := getSearchRequest(search)
	if err != nil {
		return nil, err
	}

	// Get the base query
	baseQuery := searchRequest["search"]

	// If no fields specified, fall back to regular search
	if len(fields) == 0 {
		return s.getFieldValuesFromSearch(ctx, search)
	}

	// Determine the max number of distinct values to return per field
	// Use search.Size if specified, otherwise default to 100
	maxValues := 100
	if search.Size.Set && search.Size.Value > 0 {
		maxValues = search.Size.Value
	}

	// Build a single efficient query using stats values() for all fields at once
	// Use limit=N to control how many distinct values are returned per field
	// Example: <baseQuery> | stats limit=100 values(field1) as field1, values(field2) as field2
	var valuesClauses []string
	for _, field := range fields {
		valuesClauses = append(valuesClauses, fmt.Sprintf("values(%s) as %s", field, field))
	}
	query := baseQuery + fmt.Sprintf(" | stats limit=%d ", maxValues) + strings.Join(valuesClauses, ", ")

	searchJobResponse, err := s.client.CreateSearchJob(query, searchRequest["earliest_time"], searchRequest["latest_time"], false, s.options.Headers, s.options.SearchBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create search job: %w", err)
	}

	// Wait for job to complete
	pollInterval := time.Duration(1) * time.Second
	maxRetries := maxRetryDoneJob
	if s.options.PollIntervalSeconds > 0 {
		pollInterval = time.Duration(s.options.PollIntervalSeconds) * time.Second
	}
	if s.options.MaxRetries > 0 {
		maxRetries = s.options.MaxRetries
	}

	isDone := false
	for tryCount := 0; tryCount < maxRetries; tryCount++ {
		select {
		case <-ctx.Done():
			_ = s.client.CancelSearchJob(searchJobResponse.Sid)
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		status, err := s.client.GetSearchStatus(searchJobResponse.Sid)
		if err != nil {
			_ = s.client.CancelSearchJob(searchJobResponse.Sid)
			return nil, err
		}

		if len(status.Entry) > 0 {
			isDone = status.Entry[0].Content.IsDone
		}
		if isDone {
			break
		}
	}

	if !isDone {
		_ = s.client.CancelSearchJob(searchJobResponse.Sid)
		return nil, fmt.Errorf("timeout waiting for splunk job")
	}

	// Get results from /results endpoint since we're using stats
	results, err := s.client.GetSearchResult(searchJobResponse.Sid, 0, 1, true)
	_ = s.client.CancelSearchJob(searchJobResponse.Sid)
	if err != nil {
		return nil, fmt.Errorf("failed to get results: %w", err)
	}

	// Extract distinct values from the single result row
	// The stats values() command returns a multivalue field (array) for each field
	result := make(map[string][]string)
	for _, field := range fields {
		result[field] = []string{} // Initialize with empty slice
	}

	if len(results.Results) > 0 {
		row := results.Results[0]
		for _, field := range fields {
			if v, ok := row[field]; ok {
				// Handle multivalue field - can be a single value or an array
				switch val := v.(type) {
				case []interface{}:
					for _, item := range val {
						result[field] = append(result[field], fmt.Sprintf("%v", item))
					}
				case string:
					if val != "" {
						result[field] = []string{val}
					}
				default:
					if val != nil {
						result[field] = []string{fmt.Sprintf("%v", val)}
					}
				}
			}
		}
	}

	return result, nil
}

// getFieldValuesFromSearch falls back to getting field values from a regular search
func (s SplunkLogSearchClient) getFieldValuesFromSearch(ctx context.Context, search *client.LogSearch) (map[string][]string, error) {
	searchResult, err := s.Get(ctx, search)
	if err != nil {
		return nil, err
	}

	return client.GetFieldValuesFromResult(ctx, searchResult, nil)
}

// GetClient returns a LogClient configured to communicate with the given Splunk endpoint.
func GetClient(options SplunkLogSearchClientOptions) (client.LogBackend, error) {

	if options.URL == "" {
		return nil, fmt.Errorf("splunk client Url is empty; set the Url option in config or pass --splunk-endpoint")
	}

	target := restapi.SplunkTarget{
		Endpoint: options.URL,
		Headers:  options.Headers,
	}

	// If headers include Authorization or other fixed headers, pass them as
	// an Auth implementation so GET requests also include those headers.
	if len(options.Auth.Header) > 0 {
		// set the Auth on the target so Get requests include the same headers
		target.Auth = httpPkg.HeaderAuth{Headers: options.Auth.Header}
	} else if len(options.Headers) > 0 {
		// Also check options.Headers for ad-hoc queries that pass headers directly
		target.Auth = httpPkg.HeaderAuth{Headers: options.Headers}
	}

	restClient, err := restapi.GetSplunkRestClient(target)
	if err != nil {
		return nil, err
	}

	client := SplunkLogSearchClient{
		client:  restClient,
		options: options,
	}

	return client, nil
}
