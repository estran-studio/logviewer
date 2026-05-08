package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/log/factory"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

// mockSearchFactory is a mock implementation of factory.SearchFactory
type mockSearchFactory struct{}

func (m *mockSearchFactory) GetSearchResult(_ context.Context, contextID string, _ []string, _ client.LogSearch, _ map[string]string) (client.LogSearchResult, error) {
	if contextID == "error" {
		return nil, errors.New("backend error")
	}
	return &mockLogSearchResult{}, nil
}

func (m *mockSearchFactory) GetSearchContext(_ context.Context, contextID string, _ []string, _ client.LogSearch, _ map[string]string) (*config.SearchContext, error) {
	if contextID == "error" {
		return nil, errors.New("context error")
	}
	return &config.SearchContext{
		Client: "mock_client",
		Search: client.LogSearch{},
	}, nil
}

func (m *mockSearchFactory) GetFieldValues(_ context.Context, contextID string, _ []string, _ client.LogSearch, fields []string, _ map[string]string) (map[string][]string, error) {
	if contextID == "error" {
		return nil, errors.New("backend error")
	}
	result := make(map[string][]string)
	for _, f := range fields {
		result[f] = []string{"value1", "value2"}
	}
	return result, nil
}

// mockLogSearchResult is a mock implementation of client.LogSearchResult
type mockLogSearchResult struct {
	client.LogSearchResult
}

func (m *mockLogSearchResult) GetEntries(_ context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	return []client.LogEntry{{Message: "test log"}}, nil, nil
}
func (m *mockLogSearchResult) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	return ty.UniSet[string]{"field1": {"value1"}}, nil, nil
}

func (m *mockLogSearchResult) Err() <-chan error {
	return nil
}

func newTestServer(_ *testing.T, cfg *config.ContextConfig, searchFactory factory.SearchFactory) *Server {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if cfg == nil {
		cfg = &config.ContextConfig{}
	}
	if searchFactory == nil {
		searchFactory = &mockSearchFactory{}
	}

	router := http.NewServeMux()
	s := &Server{
		config:        cfg,
		router:        router,
		logger:        logger,
		searchFactory: searchFactory,
		openapiSpec:   []byte("openapi: 3.0.0"), // a dummy spec for testing
	}
	s.routes()
	return s
}

func TestHealthHandler(t *testing.T) {
	s := newTestServer(t, nil, nil)

	req, err := http.NewRequest("GET", "/health", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")

	expected := `{"status":"ok"}` + "\n"
	assert.JSONEq(t, expected, rr.Body.String(), "handler returned unexpected body")
}

func TestContextsHandler_List(t *testing.T) {
	cfg := &config.ContextConfig{
		Contexts: map[string]config.SearchContext{
			"ctx1": {Client: "client1", SearchInherit: []string{"s1"}},
			"ctx2": {Client: "client2"},
		},
	}
	s := newTestServer(t, cfg, nil)

	req, err := http.NewRequest("GET", "/contexts", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp ContextsResponse
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Contexts, 2)
}

func TestContextsHandler_Detail(t *testing.T) {
	cfg := &config.ContextConfig{
		Contexts: map[string]config.SearchContext{
			"ctx1": {Client: "client1", SearchInherit: []string{"s1"}},
		},
	}
	s := newTestServer(t, cfg, nil)

	req, err := http.NewRequest("GET", "/contexts/ctx1", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp ContextInfo
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "ctx1", resp.ID)
}

func TestQueryLogsHandler(t *testing.T) {
	cfg := &config.ContextConfig{
		Contexts: map[string]config.SearchContext{"ctx1": {Client: "c1"}},
		Clients:  map[string]config.Client{"c1": {Type: "mock"}},
	}
	s := newTestServer(t, cfg, &mockSearchFactory{})

	body := `{"contextId": "ctx1"}`
	req, err := http.NewRequest("POST", "/query/logs", strings.NewReader(body))
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp LogsResponse
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Logs, 1)
	assert.Equal(t, "test log", resp.Logs[0].Message)
}

func TestQueryFieldsHandler(t *testing.T) {
	cfg := &config.ContextConfig{
		Contexts: map[string]config.SearchContext{"ctx1": {Client: "c1"}},
		Clients:  map[string]config.Client{"c1": {Type: "mock"}},
	}
	s := newTestServer(t, cfg, &mockSearchFactory{})

	body := `{"contextId": "ctx1"}`
	req, err := http.NewRequest("POST", "/query/fields", strings.NewReader(body))
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp FieldsResponse
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Fields, 1)
	assert.Equal(t, []string{"value1"}, resp.Fields["field1"])
}

func TestQueryLogsHandler_ValidationError(t *testing.T) {
	s := newTestServer(t, &config.ContextConfig{}, &mockSearchFactory{})

	body := `{"contextId": "nonexistent"}` // This context doesn't exist
	req, err := http.NewRequest("POST", "/query/logs", strings.NewReader(body))
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestQueryLogsHandler_BackendError(t *testing.T) {
	cfg := &config.ContextConfig{
		Contexts: map[string]config.SearchContext{"error": {Client: "c1"}},
		Clients:  map[string]config.Client{"c1": {Type: "mock"}},
	}
	s := newTestServer(t, cfg, &mockSearchFactory{})

	body := `{"contextId": "error"}`
	req, err := http.NewRequest("POST", "/query/logs", strings.NewReader(body))
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code) // The handler returns invalid search on backend error
}

func TestOpenAPIHandler(t *testing.T) {
	s := newTestServer(t, nil, nil)

	req, err := http.NewRequest("GET", "/openapi.yaml", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/yaml", rr.Header().Get("Content-Type"))

	// Check if the body is not empty and looks like a yaml
	body := rr.Body.String()
	assert.NotEmpty(t, body)
	assert.Contains(t, body, "openapi: 3.0.0")
}

func TestQueryLogsGETHandler(t *testing.T) {
	cfg := &config.ContextConfig{
		Contexts: map[string]config.SearchContext{"ctx1": {Client: "c1"}},
		Clients:  map[string]config.Client{"c1": {Type: "mock"}},
	}
	s := newTestServer(t, cfg, &mockSearchFactory{})

	req, err := http.NewRequest("GET", "/query/logs?contextId=ctx1&size=10&fields=level=error", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp LogsResponse
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Logs, 1)
	assert.Equal(t, "test log", resp.Logs[0].Message)
}

func TestQueryFieldsGETHandler(t *testing.T) {
	cfg := &config.ContextConfig{
		Contexts: map[string]config.SearchContext{"ctx1": {Client: "c1"}},
		Clients:  map[string]config.Client{"c1": {Type: "mock"}},
	}
	s := newTestServer(t, cfg, &mockSearchFactory{})

	req, err := http.NewRequest("GET", "/query/fields?contextId=ctx1", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp FieldsResponse
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Fields, 1)
}
