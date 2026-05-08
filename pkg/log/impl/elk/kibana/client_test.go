package kibana

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/estran-studio/logviewer/pkg/http"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
	"github.com/estran-studio/logviewer/pkg/log/impl/elk"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

type MockHTTPClient struct {
	OnPostJSON func(path string, headers ty.MS, body interface{}, responseData interface{}, auth http.Auth) error
}

func (m *MockHTTPClient) PostJSON(path string, headers ty.MS, body interface{}, responseData interface{}, auth http.Auth) error {
	if m.OnPostJSON != nil {
		return m.OnPostJSON(path, headers, body, responseData, auth)
	}
	return nil
}

func TestKibanaClient_Get(t *testing.T) {
	mockHTTP := &MockHTTPClient{
		OnPostJSON: func(path string, headers ty.MS, body interface{}, responseData interface{}, auth http.Auth) error {
			assert.Equal(t, "/internal/search/es", path)
			
			// Simulate response
			resp := responseData.(*SearchResponse)
			resp.RawResponse.Hits = elk.Hits{
				Hits: []elk.Hit{
					{
						Source: ty.MI{
							"message": "test log",
							"@timestamp": "2023-01-01T12:00:00Z",
						},
					},
				},
			}
			return nil
		},
	}

	kc := kibanaClient{
		target: Target{Endpoint: "http://kibana:5601"},
		client: mockHTTP,
	}

	search := &client.LogSearch{
		Options: ty.MI{
			"index": "log-index",
		},
		Range: client.SearchRange{
			Last: ty.OptWrap("15m"),
		},
	}

	result, err := kc.Get(context.Background(), search)
	assert.NoError(t, err)

	entries, _, err := result.GetEntries(context.Background())
	assert.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "test log", entries[0].Message)
}

func TestKibanaClient_GetFieldValues(t *testing.T) {
	mockHTTP := &MockHTTPClient{
		OnPostJSON: func(path string, headers ty.MS, body interface{}, responseData interface{}, auth http.Auth) error {
			// Return one hit with field
			resp := responseData.(*SearchResponse)
			resp.RawResponse.Hits = elk.Hits{
				Hits: []elk.Hit{
					{
						Source: ty.MI{
							"message": "msg1",
							"level": "INFO",
							"@timestamp": "2023-01-01T12:00:00Z",
						},
					},
				},
			}
			return nil
		},
	}

	kc := kibanaClient{
		target: Target{Endpoint: "http://kibana:5601"},
		client: mockHTTP,
	}

	search := &client.LogSearch{
		Options: ty.MI{"index": "idx"},
		Range: client.SearchRange{Last: ty.OptWrap("1h")},
	}

	values, err := kc.GetFieldValues(context.Background(), search, []string{"level"})
	assert.NoError(t, err)
	assert.Contains(t, values, "level")
	assert.Contains(t, values["level"], "INFO")
}

func TestKibanaClient_Get_MissingIndex(t *testing.T) {
	kc := kibanaClient{
		target: Target{Endpoint: "http://kibana:5601"},
		client: &MockHTTPClient{},
	}

	search := &client.LogSearch{
		Options: ty.MI{}, // No index
	}

	_, err := kc.Get(context.Background(), search)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "index is not provided")
}

func TestBuildKibanaQuery(t *testing.T) {
	// Basic test for query builder to ensure it doesn't panic
	// and returns expected structure for simple cases
	
	// Test AND logic
	f := &client.Filter{
		Logic: client.LogicAnd,
		Filters: []client.Filter{
			{Field: "level", Op: operator.Equals, Value: "ERROR"},
			{Field: "app", Op: operator.Equals, Value: "myapp"},
		},
	}
	
	q := buildKibanaQuery(f)
	assert.NotNil(t, q)
	
	// Verify JSON structure somewhat (marshalling is easiest way to inspect deep map)
	b, _ := json.Marshal(q)
	assert.Contains(t, string(b), "must")
	assert.Contains(t, string(b), "term")
	assert.Contains(t, string(b), "level")
	assert.Contains(t, string(b), "ERROR")
}
