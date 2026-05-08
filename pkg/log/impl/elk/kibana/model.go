package kibana

import (
	"github.com/estran-studio/logviewer/pkg/log/impl/elk"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// Body represents the body of a Kibana/Elasticsearch search request.
type Body struct {
	Size           int      `json:"size"`
	Sort           []ty.MI  `json:"sort"`
	Aggs           ty.MI    `json:"aggs,omitempty"`
	StoredFields   []string `json:"stored_fields,omitempty"`
	DocValueFields []ty.MI  `json:"docvalue_fields,omitempty"`
	Source         ty.MI    `json:"_source,omitempty"`
	Query          ty.MI    `json:"query"`
}

// Params represents the parameters of a Kibana/Elasticsearch search request.
type Params struct {
	Index string `json:"index"`
	Body  Body   `json:"body"`
}

// SearchRequest represents a Kibana/Elasticsearch search request.
type SearchRequest struct {
	Params Params `json:"params"`
}

// Response represents the response from a Kibana/Elasticsearch search request.
type Response struct {
	Hits elk.Hits `json:"hits"`
}

// SearchResponse represents the full Kibana/Elasticsearch search response.
type SearchResponse struct {
	RawResponse Response `json:"rawResponse"`
}
