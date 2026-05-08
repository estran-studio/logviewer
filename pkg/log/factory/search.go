// Package factory provides helpers to construct search and client factories
// used across the application.
package factory

import (
	"context"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
)

// SearchFactory exposes methods to construct or retrieve search contexts
// and results for a given search request.
type SearchFactory interface {
	GetSearchResult(ctx context.Context, contextID string, inherits []string, logSearch client.LogSearch, runtimeVars map[string]string) (client.LogSearchResult, error)
	GetSearchContext(ctx context.Context, contextID string, inherits []string, logSearch client.LogSearch, runtimeVars map[string]string) (*config.SearchContext, error)
	// GetFieldValues returns distinct values for the specified fields.
	// If fields is empty, returns values for all fields found in the logs.
	GetFieldValues(ctx context.Context, contextID string, inherits []string, logSearch client.LogSearch, fields []string, runtimeVars map[string]string) (map[string][]string, error)
}

type logSearchFactory struct {
	clientsFactory  LogBackendFactory
	searchesContext config.Contexts

	config config.ContextConfig
}

func (sf *logSearchFactory) GetSearchContext(_ context.Context, contextID string, inherits []string, logSearch client.LogSearch, runtimeVars map[string]string) (*config.SearchContext, error) {
	searchContext, err := sf.config.GetSearchContext(contextID, inherits, logSearch, runtimeVars)
	if err != nil {
		return nil, err
	}
	return &searchContext, nil
}

func (sf *logSearchFactory) GetSearchResult(ctx context.Context, contextID string, inherits []string, logSearch client.LogSearch, runtimeVars map[string]string) (client.LogSearchResult, error) {

	searchContext, err := sf.config.GetSearchContext(contextID, inherits, logSearch, runtimeVars)
	if err != nil {
		return nil, err
	}

	logClient, err := sf.clientsFactory.Get(searchContext.Client)
	if err != nil {
		return nil, err
	}

	// Merge client options into search options so clients can access client-level
	// configuration (e.g., paths, preferNativeDriver for local/ssh clients)
	sf.mergeClientOptions(&searchContext.Search, searchContext.Client)

	sr, err := (*logClient).Get(ctx, &searchContext.Search)

	return sr, err
}

func (sf *logSearchFactory) GetFieldValues(ctx context.Context, contextID string, inherits []string, logSearch client.LogSearch, fields []string, runtimeVars map[string]string) (map[string][]string, error) {
	searchContext, err := sf.config.GetSearchContext(contextID, inherits, logSearch, runtimeVars)
	if err != nil {
		return nil, err
	}

	logClient, err := sf.clientsFactory.Get(searchContext.Client)
	if err != nil {
		return nil, err
	}

	// Merge client options into search options
	sf.mergeClientOptions(&searchContext.Search, searchContext.Client)

	return (*logClient).GetFieldValues(ctx, &searchContext.Search, fields)
}

// mergeClientOptions merges client-level options (e.g., paths, preferNativeDriver)
// into the search options. Client options are merged first so search options can
// override them if needed.
func (sf *logSearchFactory) mergeClientOptions(search *client.LogSearch, clientName string) {
	clientConfig, ok := sf.config.Clients[clientName]
	if !ok {
		return
	}

	if search.Options == nil {
		search.Options = make(map[string]interface{})
	}

	// Merge client options into search options (client options first, search can override)
	for key, value := range clientConfig.Options {
		// Only set if not already present in search options
		if _, exists := search.Options[key]; !exists {
			search.Options[key] = value
		}
	}
}

// GetLogSearchFactory creates a new search factory from the given client factory and config.
func GetLogSearchFactory(
	f LogBackendFactory,
	c config.ContextConfig,
) (SearchFactory, error) {

	factory := new(logSearchFactory)
	factory.searchesContext = make(config.Contexts)
	factory.clientsFactory = f
	factory.config = c

	return factory, nil
}
