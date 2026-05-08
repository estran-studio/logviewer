// Package restapi provides a REST client for interacting with the Splunk API.
package restapi

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/estran-studio/logviewer/pkg/http"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// SearchJobResponse holds the response for a search job creation.
type SearchJobResponse struct {
	Sid string `json:"sid"`
}

// JobStatusResponse holds the response for a search job status check.
type JobStatusResponse struct {
	Entry []struct {
		Content struct {
			IsDone bool `json:"isDone"`
		} `json:"content"`
	} `json:"entry"`
}

// SearchResultsResponse holds the response for search results.
type SearchResultsResponse struct {
	Results []ty.MI `json:"results"`
}

// SplunkTarget describes the connection target for a Splunk client.
type SplunkTarget struct {
	Endpoint string `json:"endpoint"`
	Headers  ty.MS
	Auth     http.Auth
}

// SplunkRestClient provides methods to interact with the Splunk REST API.
type SplunkRestClient struct {
	target SplunkTarget
	client http.Client
}

// CreateSearchJob creates a new search job in Splunk.
func (src SplunkRestClient) CreateSearchJob(
	searchQuery string,
	earliestTime string,
	latestTime string,
	isFollow bool, // <-- Add isFollow parameter
	headers ty.MS,
	data ty.MS,
) (SearchJobResponse, error) {
	var searchJobResponse SearchJobResponse

	searchPath := "/search/jobs"

	// Ensure data map is initialized
	if data == nil {
		data = ty.MS{}
	}

	// Build the form data for the search job in a small helper so tests can
	// validate its shape without performing HTTP calls.
	body := buildSearchJobData(searchQuery, earliestTime, latestTime, isFollow, data) // <-- Pass isFollow

	err := src.client.PostData(searchPath, headers, body, &searchJobResponse, src.target.Auth)

	return searchJobResponse, err
}

// buildSearchJobData returns the form body to POST to Splunk to create a
// search job. It defaults empty time ranges to -24h@h / now and only sets the
// standard earliest_time/latest_time fields (avoids custom.dispatch.*).
func buildSearchJobData(searchQuery, earliestTime, latestTime string, isFollow bool, data ty.MS) ty.MS {
	if data == nil {
		data = ty.MS{}
	}

	if isFollow {
		data["search_mode"] = "realtime"
		// For real-time searches, "rt" prefix is used. If the user provided a
		// relative time, we assume it's for real-time. Otherwise, default to a
		// small window.
		if earliestTime == "" {
			earliestTime = "rt-5m"
		} else if !strings.HasPrefix(earliestTime, "rt") {
			earliestTime = "rt" + earliestTime
		}
		if latestTime == "" {
			latestTime = "rt"
		} else if !strings.HasPrefix(latestTime, "rt") {
			latestTime = "rt" + latestTime
		}
	} else if earliestTime == "" && latestTime == "" {
		earliestTime = "-24h@h"
		latestTime = "now"
	}

	if latestTime != "" {
		data["latest_time"] = latestTime
	}

	if earliestTime != "" {
		data["earliest_time"] = earliestTime
	}

	data["search"] = "search " + searchQuery
	return data
}

// CancelSearchJob cancels a running search job in Splunk.
func (src SplunkRestClient) CancelSearchJob(sid string) error {
	searchPath := fmt.Sprintf("/search/jobs/%s", sid)
	err := src.client.Delete(searchPath, src.target.Headers, src.target.Auth)
	if err != nil {
		return fmt.Errorf("failed to cancel splunk search job %s: %w", sid, err)
	}
	log.Printf("splunk search job %s cancelled", sid)
	return nil
}

// GetSearchStatus retrieves the status of a search job in Splunk.
func (src SplunkRestClient) GetSearchStatus(
	sid string,
) (JobStatusResponse, error) {
	var response JobStatusResponse

	searchPath := fmt.Sprintf("/search/jobs/%s", sid)

	queryParams := ty.MS{
		"output_mode": "json",
	}

	err := src.client.Get(searchPath, queryParams, src.target.Headers, nil, &response, src.target.Auth)
	if err == nil {
		if len(response.Entry) > 0 {
			if http.DebugEnabled() {
				log.Printf("[SPLUNK-STATUS] entry_count=%d isDone=%v\n", len(response.Entry), response.Entry[0].Content.IsDone)
			}
		} else {
			if http.DebugEnabled() {
				log.Printf("[SPLUNK-STATUS] entry_count=0\n")
			}
		}
	}
	return response, err
}

// GetSearchResult retrieves the results of a search job in Splunk.
func (src SplunkRestClient) GetSearchResult(
	sid string,
	offset int,
	count int,
	useResultsEndpoint bool,
) (SearchResultsResponse, error) {
	var response SearchResultsResponse

	// Use /results for transforming commands (stats, chart, etc.)
	// Use /events for regular log searches
	endpoint := "events"
	if useResultsEndpoint {
		endpoint = "results"
	}
	searchPath := fmt.Sprintf("/search/jobs/%s/%s", sid, endpoint)

	queryParams := ty.MS{
		"output_mode": "json",
		"offset":      strconv.Itoa(offset),
		"count":       strconv.Itoa(count),
	}

	err := src.client.Get(searchPath, queryParams, src.target.Headers, nil, &response, src.target.Auth)
	return response, err

}

// GetSplunkRestClient returns a SplunkRestClient configured to communicate with the given endpoint.
func GetSplunkRestClient(
	target SplunkTarget,
) (SplunkRestClient, error) {
	if target.Endpoint == "" {
		return SplunkRestClient{}, fmt.Errorf("splunk endpoint is empty; provide a valid URL in the client configuration or via --splunk-endpoint")
	}

	return SplunkRestClient{
		target: target,
		client: http.GetClient(target.Endpoint, nil),
	}, nil
}
