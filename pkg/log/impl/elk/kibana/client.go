// Package kibana contains a client implementation for Kibana/Elasticsearch
// based backends used to retrieve logs via Kibana's internal search API.
package kibana

import (
	"context"
	"errors"

	"github.com/estran-studio/logviewer/pkg/http"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
	"github.com/estran-studio/logviewer/pkg/log/impl/elk"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// HTTPClient defines the subset of the HTTP client interface used by this package.
type HTTPClient interface {
	PostJSON(path string, headers ty.MS, body interface{}, responseData interface{}, auth http.Auth) error
}

// Target describes the connection target for a Kibana-backed client.
type Target struct {
	Endpoint string `json:"endpoint"`
}

type kibanaClient struct {
	target Target
	client HTTPClient
}

func (kc kibanaClient) Get(_ context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
	var searchResponse SearchResponse

	request, err := getSearchRequest(search)
	if err != nil {
		return nil, err
	}

	err = kc.client.PostJSON("/internal/search/es", ty.MS{
		"kbn-version": search.Options.GetOr("version", "7.10.2").(string),
	}, &request, &searchResponse, nil)
	if err != nil {
		return nil, err
	}

	return elk.NewSearchResult(&kc, search, searchResponse.RawResponse.Hits), nil
}

// buildKibanaCondition builds a single Kibana query condition from a filter leaf.
func buildKibanaCondition(f *client.Filter) ty.MI {
	if f.Field == "" {
		return nil
	}

	op := f.Op
	if op == "" {
		op = operator.Match
	}

	// Handle special "_" sentinel for full-text search
	field := f.Field
	if field == "_" {
		field = "_all"
	}

	var condition ty.MI

	switch op {
	case operator.Regex:
		condition = ty.MI{
			"regexp": ty.MI{
				field: f.Value,
			},
		}
	case operator.Wildcard:
		condition = ty.MI{
			"wildcard": ty.MI{
				field: f.Value,
			},
		}
	case operator.Exists:
		condition = ty.MI{
			"exists": ty.MI{
				"field": field,
			},
		}
	case operator.Equals:
		condition = ty.MI{
			"term": ty.MI{
				field: f.Value,
			},
		}
	case operator.Gt:
		condition = ty.MI{
			"range": ty.MI{
				field: ty.MI{
					"gt": f.Value,
				},
			},
		}
	case operator.Gte:
		condition = ty.MI{
			"range": ty.MI{
				field: ty.MI{
					"gte": f.Value,
				},
			},
		}
	case operator.Lt:
		condition = ty.MI{
			"range": ty.MI{
				field: ty.MI{
					"lt": f.Value,
				},
			},
		}
	case operator.Lte:
		condition = ty.MI{
			"range": ty.MI{
				field: ty.MI{
					"lte": f.Value,
				},
			},
		}
	default: // match - use match_phrase for Kibana (default behavior)
		condition = ty.MI{
			"match_phrase": ty.MI{
				field: f.Value,
			},
		}
	}

	// Handle negation
	if f.Negate {
		return ty.MI{
			"bool": ty.MI{
				"must_not": []ty.MI{condition},
			},
		}
	}

	return condition
}

// buildKibanaQuery recursively builds a Kibana bool query from a Filter AST.
func buildKibanaQuery(f *client.Filter) ty.MI {
	if f == nil {
		return nil
	}

	// Handle Leaf (Condition)
	if f.Field != "" {
		return buildKibanaCondition(f)
	}

	// Handle Branch (Group)
	if f.Logic == "" || len(f.Filters) == 0 {
		return nil
	}

	var clauses []ty.MI
	for _, child := range f.Filters {
		clause := buildKibanaQuery(&child)
		if clause != nil {
			clauses = append(clauses, clause)
		}
	}

	if len(clauses) == 0 {
		return nil
	}

	// If only one clause and AND, return it directly
	if len(clauses) == 1 && f.Logic == client.LogicAnd {
		return clauses[0]
	}

	switch f.Logic {
	case client.LogicAnd:
		return ty.MI{
			"bool": ty.MI{
				"must": clauses,
			},
		}
	case client.LogicOr:
		return ty.MI{
			"bool": ty.MI{
				"should":               clauses,
				"minimum_should_match": 1,
			},
		}
	case client.LogicNot:
		return ty.MI{
			"bool": ty.MI{
				"must_not": clauses,
			},
		}
	}

	return nil
}

func getSearchRequest(search *client.LogSearch) (SearchRequest, error) {
	request := SearchRequest{}

	index := search.Options.GetString("index")

	if index == "" {
		return request, errors.New("index is not provided for kibana log client")
	}

	gte, lte, err := elk.GetDateRange(search)
	if err != nil {
		return SearchRequest{}, err
	}

	request.Params.Index = index
	request.Params.Body.Size = search.Size.Value
	request.Params.Body.Sort = []ty.MI{
		{
			"@timestamp": ty.MI{
				"order":         "desc",
				"unmapped_type": "boolean",
			},
		},
	}
	request.Params.Body.StoredFields = []string{"*"}
	request.Params.Body.DocValueFields = []ty.MI{
		{
			"field":  "@timestamp",
			"format": "date_time",
		},
	}

	request.Params.Body.Source = ty.MI{
		"excludes": []interface{}{},
	}

	// Build conditions from the effective filter
	conditions := []ty.MI{
		{"match_all": ty.MI{}},
	}

	effectiveFilter := search.GetEffectiveFilter()
	if effectiveFilter != nil {
		filterQuery := buildKibanaQuery(effectiveFilter)
		if filterQuery != nil {
			conditions = append(conditions, filterQuery)
		}
	}

	// Add timestamp range
	conditions = append(conditions, elk.GetDateRangeConditon(gte, lte))

	request.Params.Body.Query = ty.MI{
		"bool": ty.MI{
			"filter": conditions,
		},
	}

	return request, nil
}

func (kc kibanaClient) GetFieldValues(ctx context.Context, search *client.LogSearch, fields []string) (map[string][]string, error) {
	// For kibana, we need to run a search and extract field values from the results
	result, err := kc.Get(ctx, search)
	if err != nil {
		return nil, err
	}
	return client.GetFieldValuesFromResult(ctx, result, fields)
}

// GetClient returns a LogClient configured to communicate with the given Kibana endpoint.
func GetClient(target Target) (client.LogBackend, error) {
	client := new(kibanaClient)
	client.target = target
	client.client = http.GetClient(target.Endpoint, nil)
	return client, nil
}
