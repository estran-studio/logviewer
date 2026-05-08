// Package opensearch provides an OpenSearch implementation of the LogClient interface.
package opensearch

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/estran-studio/logviewer/pkg/http"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/impl/elk"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// Target describes the connection target for an OpenSearch-backed client.
type Target struct {
	Endpoint string `json:"endpoint"`
}

type openSearchClient struct {
	target Target
	client http.Client
}

func (kc openSearchClient) Get(_ context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
	var searchResult SearchResult

	index := search.Options.GetString("index")

	if index == "" {
		return nil, errors.New("index is not provided for opensearch log client")
	}

	request, err := GetSearchRequest(search)
	if err != nil {
		return nil, err
	}

	err = kc.client.Get(fmt.Sprintf("/%s/_search", index), ty.MS{}, ty.MS{}, &request, &searchResult, nil)
	if err != nil {
		return nil, err
	}

	res := elk.NewSearchResult(&kc, search, searchResult.Hits)

	// If a page token was provided we already validated and parsed it in
	// GetSearchRequest; reuse that value for pagination calculation.
	if search.PageToken.Set && search.PageToken.Value != "" {
		if parsedOffset, err := strconv.Atoi(search.PageToken.Value); err == nil {
			res.CurrentOffset = parsedOffset
		} else {
			// Shouldn't happen because GetSearchRequest already validated the token,
			// but guard defensively.
			return nil, fmt.Errorf("invalid page token: %w", err)
		}
	}

	return res, nil
}

func (kc openSearchClient) GetFieldValues(ctx context.Context, search *client.LogSearch, fields []string) (map[string][]string, error) {
	index := search.Options.GetString("index")

	if index == "" {
		return nil, errors.New("index is not provided for opensearch log client")
	}

	// If no fields specified, fall back to getting all fields from a regular search
	if len(fields) == 0 {
		return kc.getFieldValuesFromSearch(ctx, search)
	}

	// Build base query
	gte, lte, err := elk.GetDateRange(search)
	if err != nil {
		return nil, err
	}

	// Build conditions from the effective filter
	var filterConditions []ty.MI

	// Add Native Query if provided
	if search.NativeQuery.Set && search.NativeQuery.Value != "" {
		filterConditions = append(filterConditions, ty.MI{
			"query_string": ty.MI{
				"query": search.NativeQuery.Value,
			},
		})
	}

	// Add effective filter conditions
	effectiveFilter := search.GetEffectiveFilter()
	if effectiveFilter != nil {
		filterQuery := buildOpenSearchQuery(effectiveFilter)
		if filterQuery != nil {
			filterConditions = append(filterConditions, ty.MI(filterQuery))
		}
	}

	// Add timestamp range condition
	timestampCondition := ty.MI{
		"range": ty.MI{
			"@timestamp": ty.MI{
				"format": "strict_date_optional_time",
				"gte":    gte,
				"lte":    lte,
			},
		},
	}
	filterConditions = append(filterConditions, timestampCondition)

	query := ty.MI{
		"bool": ty.MI{
			"must": filterConditions,
		},
	}

	// Determine the max number of distinct values to return per field
	// Use search.Size if specified, otherwise default to 100
	maxValues := 100
	if search.Size.Set && search.Size.Value > 0 {
		maxValues = search.Size.Value
	}

	// Build aggregations for each field
	aggs := ty.MI{}
	for _, field := range fields {
		// Use .keyword suffix for text fields to enable aggregation
		// This is required in OpenSearch/Elasticsearch for analyzed text fields
		fieldName := field
		if !strings.HasSuffix(field, ".keyword") {
			fieldName = field + ".keyword"
		}
		aggs[field+"_values"] = ty.MI{
			"terms": ty.MI{
				"field": fieldName,
				"size":  maxValues,
			},
		}
	}

	// Build the request with size=0 (we only want aggregations, not hits)
	request := ty.MI{
		"query": query,
		"size":  0,
		"aggs":  aggs,
	}

	var response struct {
		Aggregations map[string]struct {
			Buckets []struct {
				Key      interface{} `json:"key"`
				DocCount int         `json:"doc_count"`
			} `json:"buckets"`
		} `json:"aggregations"`
	}

	err = kc.client.Get(fmt.Sprintf("/%s/_search", index), ty.MS{}, ty.MS{}, &request, &response, nil)
	if err != nil {
		return nil, err
	}

	// Extract values from aggregations
	result := make(map[string][]string)
	for _, field := range fields {
		aggKey := field + "_values"
		if agg, ok := response.Aggregations[aggKey]; ok {
			var values []string
			for _, bucket := range agg.Buckets {
				values = append(values, fmt.Sprintf("%v", bucket.Key))
			}
			result[field] = values
		} else {
			result[field] = []string{}
		}
	}

	return result, nil
}

// getFieldValuesFromSearch falls back to getting field values from a regular search
func (kc openSearchClient) getFieldValuesFromSearch(ctx context.Context, search *client.LogSearch) (map[string][]string, error) {
	searchResult, err := kc.Get(ctx, search)
	if err != nil {
		return nil, err
	}

	return client.GetFieldValuesFromResult(ctx, searchResult, nil)
}

// GetClient returns a LogClient configured to communicate with the given OpenSearch endpoint.
func GetClient(target Target) (client.LogBackend, error) {
	client := new(openSearchClient)
	client.target = target
	client.client = http.GetClient(target.Endpoint, nil)
	return client, nil
}
