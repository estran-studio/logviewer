package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/factory"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// ConfiguredLogClient implements LogClient for config-based (multi-context) searches.
type ConfiguredLogClient struct {
	Factory     factory.SearchFactory
	ContextIDs  []string
	Inherits    []string
	RuntimeVars map[string]string
}

func (c *ConfiguredLogClient) Query(ctx context.Context, search client.LogSearch) ([]client.LogEntry, error) {
	// For single context, execute directly
	if len(c.ContextIDs) == 1 {
		search.Options["__context_id__"] = c.ContextIDs[0]
		result, err := c.Factory.GetSearchResult(ctx, c.ContextIDs[0], c.Inherits, search, c.RuntimeVars)
		if err != nil {
			return nil, err
		}
		return consumeSearchResult(ctx, result)
	}

	// Fan-out for multiple contexts
	multiResult, err := client.NewMultiLogSearchResult(&search)
	if err != nil {
		return nil, err
	}
	var wg sync.WaitGroup

	for _, contextID := range c.ContextIDs {
		wg.Add(1)
		go func(cid string) {
			defer wg.Done()
			reqCopy := search
			// Deep copy maps
			reqCopy.Options = ty.MergeM(make(ty.MI, len(search.Options)+1), search.Options)
			reqCopy.Options["__context_id__"] = cid
			reqCopy.Fields = ty.MergeM(make(ty.MS, len(search.Fields)), search.Fields)
			reqCopy.FieldsCondition = ty.MergeM(make(ty.MS, len(search.FieldsCondition)), search.FieldsCondition)
		
			if search.Variables != nil {
				reqCopy.Variables = make(map[string]client.VariableDefinition, len(search.Variables))
				for k, v := range search.Variables {
					reqCopy.Variables[k] = v
				}
			}

			sr, err := c.Factory.GetSearchResult(ctx, cid, c.Inherits, reqCopy, c.RuntimeVars)
			multiResult.Add(sr, err)
		}(contextID)
	}
	wg.Wait()

	return consumeSearchResult(ctx, multiResult)
}

func consumeSearchResult(ctx context.Context, result client.LogSearchResult) ([]client.LogEntry, error) {
	entries, ch, err := result.GetEntries(ctx)
	if err != nil {
		return nil, err
	}
	if ch != nil {
		for batch := range ch {
			entries = append(entries, batch...)
		}
	}
	return entries, nil
}

func (c *ConfiguredLogClient) GetFields(ctx context.Context, search client.LogSearch) (map[string][]string, error) {
	// Similar fan-out logic for fields
	allFields := make(ty.UniSet[string])
	var mu sync.Mutex
	var wg sync.WaitGroup
	var hasError bool

	for _, contextID := range c.ContextIDs {
		wg.Add(1)
		go func(cid string) {
			defer wg.Done()
			// Note: SearchFactory doesn't expose GetFields directly with runtimeVars,
			// it uses GetSearchResult -> GetFields.
			reqCopy := search
			reqCopy.Options = ty.MergeM(make(ty.MI), search.Options)
			reqCopy.Options["__context_id__"] = cid
			
			sr, err := c.Factory.GetSearchResult(ctx, cid, c.Inherits, reqCopy, c.RuntimeVars)
			if err != nil {
				mu.Lock()
				hasError = true
				mu.Unlock()
				return
			}
			
			fields, ch, err := sr.GetFields(ctx)
			if err != nil {
				mu.Lock()
				hasError = true
				mu.Unlock()
				return
			}
			
			mu.Lock()
			if fields != nil {
				for k, v := range fields {
					for _, val := range v {
						allFields.Add(k, val)
					}
				}
			}
			mu.Unlock()

			if ch != nil {
				for batch := range ch {
					mu.Lock()
					for k, v := range batch {
						for _, val := range v {
							allFields.Add(k, val)
						}
					}
					mu.Unlock()
				}
			}
		}(contextID)
	}
	wg.Wait()

	if hasError && len(allFields) == 0 {
		return nil, errors.New("failed to get fields from all contexts")
	}

	return allFields, nil
}

func (c *ConfiguredLogClient) GetValues(ctx context.Context, search client.LogSearch, field string) ([]string, error) {
	// Fan-out for values
	allValues := make(map[string]struct{})
	var mu sync.Mutex
	var wg sync.WaitGroup
	var hasError bool

	for _, contextID := range c.ContextIDs {
		wg.Add(1)
		go func(cid string) {
			defer wg.Done()
			valsMap, err := c.Factory.GetFieldValues(ctx, cid, c.Inherits, search, []string{field}, c.RuntimeVars)
			if err != nil {
				mu.Lock()
				hasError = true
				mu.Unlock()
				return
			}
			
			if vals, ok := valsMap[field]; ok {
				mu.Lock()
				for _, v := range vals {
					allValues[v] = struct{}{}
				}
				mu.Unlock()
			}
		}(contextID)
	}
	wg.Wait()

	if hasError && len(allValues) == 0 {
		return nil, errors.New("failed to get field values")
	}

	result := make([]string, 0, len(allValues))
	for v := range allValues {
		result = append(result, v)
	}
	sort.Strings(result)
	return result, nil
}

// resolveLogClient determines the appropriate LogClient based on flags/config.
func resolveLogClient() (client.LogClient, client.LogSearch, error) {
	searchRequest := buildSearchRequest()

	// 1. Ad-Hoc
	if isAdHocQuery() {
		backend, err := getAdHocLogClient(&searchRequest)
		if err != nil {
			return nil, searchRequest, err
		}
		return client.NewBackendAdapter(backend), searchRequest, nil
	}

	// 2. Config-based
	if configPath == "" && len(contextIDs) == 0 {
		return nil, searchRequest, errors.New("no config or context specified; use -i to select a context or provide endpoint flags")
	}

	cfg, _, err := loadConfig(configPath)
	if err != nil {
		return nil, searchRequest, err
	}

	backendFactory, err := factory.GetLogBackendFactory(cfg.Clients)
	if err != nil {
		return nil, searchRequest, err
	}

	searchFactory, err := factory.GetLogSearchFactory(backendFactory, *cfg)
	if err != nil {
		return nil, searchRequest, err
	}

	runtimeVars := parseRuntimeVars()
	resolvedContextIDs := resolveContextIDsFromConfig(cfg)

	if len(resolvedContextIDs) == 0 {
		return nil, searchRequest, errors.New("no context specified; use -i to select a context")
	}

	return &ConfiguredLogClient{
		Factory:     searchFactory,
		ContextIDs:  resolvedContextIDs,
		Inherits:    inherits,
		RuntimeVars: runtimeVars,
	}, searchRequest, nil
}

// RunQueryValues executes the 'query values' logic using a LogClient.
func RunQueryValues(out io.Writer, cli client.LogClient, search client.LogSearch, fields []string, asJSON bool) error {
	ctx := context.Background()
	results := make(map[string][]string)

	for _, field := range fields {
		values, err := cli.GetValues(ctx, search, field)
		if err != nil {
			return fmt.Errorf("error getting values for field %s: %w", field, err)
		}
		results[field] = values
	}

	if asJSON {
		enc := json.NewEncoder(out)
		// Maintain map order? JSON map order is undefined, but for testing we might care.
		return enc.Encode(results)
	}

	// Text output
	for _, field := range fields {
		values := results[field]
		if len(values) == 0 {
			fmt.Fprintf(out, "%s \n    (no values found)\n", field)
			continue
		}
		fmt.Fprintf(out, "%s \n", field)
		for _, v := range values {
			fmt.Fprintf(out, "    %s\n", v)
		}
	}
	return nil
}

// RunQueryField executes the 'query field' logic using a LogClient.
func RunQueryField(out io.Writer, cli client.LogClient, search client.LogSearch, asJSON bool) error {
	ctx := context.Background()
	fields, err := cli.GetFields(ctx, search)
	if err != nil {
		return err
	}

	if asJSON {
		// Output as JSON for machine consumption
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(fields)
	}

	// Human-readable output
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		b := fields[k]
		fmt.Fprintf(out, "%s \n", k)
		for _, r := range b {
			fmt.Fprintf(out, "    %s\n", r)
		}
	}
	return nil
}