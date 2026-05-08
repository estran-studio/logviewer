package cmd

// -----------------------------------------------------------------------------
// Future Improvements (MCP Server)
// -----------------------------------------------------------------------------
// 1. Streaming / Follow Mode:
//    - Add a tool (e.g. "tail_logs" or "query_logs_follow") that streams new log
//      entries for a context (server-sent incremental batches or a bounded poll
//      loop). Would require MCP extension for incremental results or chunked
//      output handling.
// 2. Summarization / Analytics Tool:
//    - Provide a "summarize_logs" tool that groups by level, extracts top error
//      signatures, counts occurrences, and surfaces anomaly hints. Could accept
//      parameters: contextID, last, groupBy (level/service), topN.
// 3. Explicit Time Range Parameters:
//    - Support gte / lte absolute timestamps (RFC3339) alongside "last" to allow
//      precise investigations and reproducibility of queries.
// 4. Aggregation / Facet Tool:
//    - Expose a "facet_fields" tool returning counts for selected fields
//      (e.g. level, service, host) to guide targeted filtering.
// 5. Structured Error Codes:
//    - Standardize JSON error envelope with machine-friendly codes
//      (e.g. CONTEXT_NOT_FOUND, BACKEND_UNAVAILABLE, VALIDATION_ERROR) instead of
//      returning plain text or heuristic detection.
// 6. Field Discovery Caching:
//    - Cache get_fields results per context + time window (LRU / TTL) to reduce
//      backend load when agents probe frequently.
// 7. Partial / Sample Queries:
//    - Allow a lightweight "sample_logs" tool that fetches a very small set
//      (e.g. size=5) quickly for faster iterative refinement.
// 8. Query DSL / Expression Language:
//    - Introduce a simple expression syntax (level=ERROR AND message~"timeout")
//      parsed into backend-specific filters to expand flexibility beyond
//      strict equality.
// 9. Security / Multi-Tenancy:
//    - Context-level ACLs and redaction hooks before returning entries
//      (mask secrets, PII, tokens).
// 10. Metrics & Instrumentation:
//     - Emit internal metrics (query latency, error rate, cache hit ratio) and
//       optionally expose via a "diagnostics" tool.
// 11. Pagination / Cursoring: ✅ COMPLETED
//     - query_logs now supports pageToken parameter and returns nextPageToken in
//       meta when more results are available. Agent can fetch subsequent pages by
//       passing the token in the next request.
// 12. Enhanced Similarity Suggestions:
//     - Replace simple Levenshtein with weighted trigram similarity and include
//       last-used context prioritization.
// 13. README / Documentation Update:
//     - Add detailed MCP usage section, examples of natural-language prompts,
//       and troubleshooting guide for context resolution.
// 14. Pluggable Normalization Pipeline:
//     - Allow custom transformers (timestamp normalization, field remapping,
//       enrichment) prior to returning entries.
// 15. Rate Limiting / Circuit Breaking:
//     - Prevent costly repeated queries (same context/filters) in tight loops.
// 16. Advanced Prompt Templates:
//     - Additional prompts: error_investigation, performance_degradation,
//       release_regression to accelerate LLM-driven diagnostics.
// 17. Cross-Context Correlation Tool:
//     - Tool that executes the same search across multiple contexts and merges
//       aligned timelines (e.g. by traceId / requestId).
// 18. Output Formatting Options:
//     - Allow user to request minimal, pretty, or raw JSON for entries.
// 19. Pluggable Authentication to External Backends:
//     - Support dynamic credentials injection or rotation for Splunk/ELK.
// 20. Test Coverage Expansion:
//     - Add integration tests specifically for MCP tool handlers with mocked
//       factories to ensure stability.
// -----------------------------------------------------------------------------

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/log/factory"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/fsnotify/fsnotify"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

// BackendGuide contains syntax documentation for a specific backend type.
type BackendGuide struct {
	Name           string
	QueryLang      string
	SyntaxGuide    string
	ExampleQueries []string
}

// backendSyntaxGuides maps backend types to their query syntax documentation.
var backendSyntaxGuides = map[string]BackendGuide{
	"splunk": {
		Name:      "Splunk",
		QueryLang: "SPL (Search Processing Language)",
		SyntaxGuide: `SPL Syntax:
- Basic search: index=main sourcetype=json
- Field filters: level=ERROR app="payment-service"
- Wildcards: message=*timeout*
- Time: earliest=-1h latest=now
- Stats: | stats count by level
- Timechart: | timechart span=1h count by level`,
		ExampleQueries: []string{
			`index=main level=ERROR | stats count by app`,
			`index=main sourcetype=json | timechart span=1h count by level`,
			`index=main message=*timeout* | top 10 app`,
		},
	},
	"opensearch": {
		Name:      "OpenSearch",
		QueryLang: "Lucene Query Syntax",
		SyntaxGuide: `Lucene Syntax:
- Field match: level:ERROR
- AND/OR: level:ERROR AND app:payment-service
- Wildcards: message:*timeout*
- Ranges: @timestamp:[2024-01-01 TO 2024-12-31]
- Exists: _exists_:trace_id`,
		ExampleQueries: []string{
			`level:ERROR OR level:WARN`,
			`app:payment-service AND message:*timeout*`,
			`level:ERROR AND NOT app:debug-service`,
		},
	},
	"kibana": {
		Name:      "Kibana",
		QueryLang: "KQL (Kibana Query Language)",
		SyntaxGuide: `KQL Syntax:
- Field match: level: ERROR
- AND/OR: level: ERROR and app: payment-service
- Wildcards: message: *timeout*
- Negation: not level: DEBUG`,
		ExampleQueries: []string{
			`level: ERROR or level: WARN`,
			`app: payment-service and message: *error*`,
		},
	},
	"k8s": {
		Name:      "Kubernetes",
		QueryLang: "Label Selectors + Client-side Filtering",
		SyntaxGuide: `Kubernetes Options:
- namespace: Target namespace
- pod: Specific pod name
- labelSelector: Match pods (e.g., "app=api,env=prod")
- container: Target container in multi-container pods
- previous: Logs from previous container instance

Filtering is done client-side on extracted fields.`,
		ExampleQueries: []string{
			`labelSelector=app=payment-processor,env=prod`,
			`pod=payment-service-abc123 container=main`,
		},
	},
	"docker": {
		Name:      "Docker",
		QueryLang: "Container Selectors",
		SyntaxGuide: `Docker Options:
- container: Container ID or name
- service: Docker Compose service name
- project: Docker Compose project name
- timestamps: Include timestamps`,
		ExampleQueries: []string{
			`container=my-app`,
			`service=api-gateway project=myproject`,
		},
	},
	"cloudwatch": {
		Name:      "CloudWatch Logs",
		QueryLang: "CloudWatch Logs Insights",
		SyntaxGuide: `Insights Syntax:
- fields @timestamp, @message
- filter @message like /error/i
- filter level = 'ERROR'
- stats count(*) by level
- sort @timestamp desc`,
		ExampleQueries: []string{
			`fields @timestamp, @message | filter level = 'ERROR' | sort @timestamp desc`,
			`stats count(*) by level | sort count desc`,
		},
	},
	"ssh": {
		Name:      "SSH",
		QueryLang: "Shell Commands",
		SyntaxGuide: `SSH executes shell commands on remote hosts.
- cmd: Shell command template
- Template variables: {{.Size.Value}}, {{.Range.Last.Value}}`,
		ExampleQueries: []string{
			`tail -f /var/log/app.log`,
			`journalctl -u myservice --since "1 hour ago"`,
		},
	},
	"local": {
		Name:      "Local",
		QueryLang: "Shell Commands",
		SyntaxGuide: `Local backend executes shell commands locally.
- cmd: Shell command template`,
		ExampleQueries: []string{
			`tail -f /var/log/syslog`,
			`cat /path/to/app.log | grep ERROR`,
		},
	},
}

var mcpPort int

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Starts a MCP server",
	Long:  `Starts a MCP server, exposing the logviewer's core functionalities as a tool.`,
	Run: func(_ *cobra.Command, _ []string) {
		// Centralized config handling (matching query command):
		// - If an explicit configPath is given, use it.
		// - If no configPath, attempt to load the default config.
		cfgPath := configPath

		// Pre-validate config loading to provide consistent error messages
		_, files, err := loadConfig(cfgPath)
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("Starting MCP server with config: %v\n", files)

		bundle, err := BuildMCPServer(cfgPath)
		if err != nil {
			log.Fatalf("failed to build MCP server: %v", err)
		}

		if err := server.ServeStdio(bundle.Server); err != nil {
			log.Fatalf("failed to start server: %v", err)
		}
	},
}

// ConfigManager handles thread-safe configuration reloading.
type ConfigManager struct {
	mu            sync.RWMutex
	configPath    string
	loadedFiles   []string
	currentCfg    *config.ContextConfig
	searchFactory factory.SearchFactory
	watcher       *fsnotify.Watcher
	debounceTimer *time.Timer
	closeChan     chan struct{}
}

// NewConfigManager creates a new ConfigManager that watches the given config path for changes.
func NewConfigManager(path string) (*ConfigManager, error) {
	cm := &ConfigManager{
		configPath: path,
		closeChan:  make(chan struct{}),
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}
	cm.watcher = watcher

	if err := cm.Reload(); err != nil {
		_ = watcher.Close()
		return nil, err
	}

	go cm.watch()

	return cm, nil
}

// NewConfigManagerForTest creates a ConfigManager from an in-memory config (for testing).
// Does not set up file watching.
func NewConfigManagerForTest(cfg *config.ContextConfig) (*ConfigManager, error) {
	clientFactory, err := factory.GetLogBackendFactory(cfg.Clients)
	if err != nil {
		return nil, fmt.Errorf("failed to build client factory: %w", err)
	}

	searchFactory, err := factory.GetLogSearchFactory(clientFactory, *cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build search factory: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	return &ConfigManager{
		currentCfg:    cfg,
		searchFactory: searchFactory,
		watcher:       watcher,
	}, nil
}

func (cm *ConfigManager) watch() {
	const debounceDelay = 100 * time.Millisecond

	for {
		select {
		case event, ok := <-cm.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
				log.Printf("Config file changed: %s", event.Name)
				// Debounce: reset timer on each event to avoid redundant reloads
				if cm.debounceTimer != nil {
					cm.debounceTimer.Stop()
				}
				cm.debounceTimer = time.AfterFunc(debounceDelay, func() {
					if err := cm.Reload(); err != nil {
						log.Printf("Error reloading config: %v", err)
					}
				})
			}
		case err, ok := <-cm.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		case <-cm.closeChan:
			return
		}
	}
}

// Reload reloads the configuration from disk and updates the search factory.
func (cm *ConfigManager) Reload() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.configPath != "" {
		log.Printf("Reloading configuration from: %s", cm.configPath)
	} else {
		log.Printf("Reloading configuration from default locations")
	}

	// 1. Reload file from disk
	newCfg, files, err := loadConfig(cm.configPath)
	if err != nil {
		return err
	}

	// 2. Rebuild factories
	clientFactory, err := factory.GetLogBackendFactory(newCfg.Clients)
	if err != nil {
		return fmt.Errorf("failed to build client factory: %w", err)
	}

	searchFactory, err := factory.GetLogSearchFactory(clientFactory, *newCfg)
	if err != nil {
		return fmt.Errorf("failed to build search factory: %w", err)
	}

	// 3. Update state
	cm.currentCfg = newCfg
	cm.searchFactory = searchFactory

	// 4. Update watcher
	// First, remove old files from watcher to prevent resource leaks
	for _, f := range cm.loadedFiles {
		// It's safe to ignore errors here, as the file might have been deleted.
		_ = cm.watcher.Remove(f)
	}
	// Then, add new files to watcher
	for _, f := range files {
		if err := cm.watcher.Add(f); err != nil {
			log.Printf("Failed to watch file %s: %v", f, err)
		}
	}
	cm.loadedFiles = files

	return nil
}

// Get returns a thread-safe snapshot of the current configuration and search factory.
func (cm *ConfigManager) Get() (*config.ContextConfig, factory.SearchFactory) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.currentCfg, cm.searchFactory
}

// Close gracefully shuts down the ConfigManager, stopping the watcher and cleaning up resources.
func (cm *ConfigManager) Close() error {
	close(cm.closeChan)
	if cm.debounceTimer != nil {
		cm.debounceTimer.Stop()
	}
	return cm.watcher.Close()
}

// MCPServerBundle contains the MCP server instance and tool handlers.
// Exposed for testing so we can spin up the server without invoking cobra.Run path.
type MCPServerBundle struct {
	Server       *server.MCPServer
	ToolHandlers map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
}

// BuildMCPServer creates an MCP server instance with all tools/resources/prompts registered.
func BuildMCPServer(configPath string) (*MCPServerBundle, error) {
	// Initialize config manager
	cm, err := NewConfigManager(configPath)
	if err != nil {
		return nil, err
	}
	return buildMCPServerWithManager(cm)
}

// buildMCPServerWithManager creates the MCP server with a provided ConfigManager.
// Internal function for testing.
//
//nolint:gocyclo // Registering multiple MCP tools/handlers in a single function
func buildMCPServerWithManager(cm *ConfigManager) (*MCPServerBundle, error) {
	s := server.NewMCPServer(
		"logviewer",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	)

	handlers := map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){}

	// --- Tool: reload_config ---
	reloadTool := mcp.NewTool("reload_config",
		mcp.WithDescription("Reload the configuration file from disk. Use this if you have modified the config.yaml file."),
	)
	reloadHandler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := cm.Reload(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Reload failed: %v", err)), nil
		}
		return mcp.NewToolResultText("Configuration successfully reloaded."), nil
	}
	s.AddTool(reloadTool, reloadHandler)
	handlers["reload_config"] = reloadHandler

	listContextsTool := mcp.NewTool("list_contexts",
		mcp.WithDescription(`List all configured log contexts.

Usage: list_contexts

Returns: JSON array of context identifiers (strings) that can be used in other tools.

Note: You don't have to call this before every query. You can attempt query_logs directly; if the contextID is invalid the server will now return suggestions including available contexts.
`),
	)
	listHandler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cfg, _ := cm.Get()
		contextIDs := make([]string, 0, len(cfg.Contexts))
		for id := range cfg.Contexts {
			contextIDs = append(contextIDs, id)
		}
		sort.Strings(contextIDs)
		jsonBytes, err := json.Marshal(contextIDs)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal contexts: %v", err)), nil
		}
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}
	s.AddTool(listContextsTool, listHandler)
	handlers["list_contexts"] = listHandler

	getFieldsTool := mcp.NewTool("get_fields",
		mcp.WithDescription(`Discover available structured log fields for a given context.

Usage: get_fields contextID=<context>

Parameters:
  contextID (string, required): Context identifier.

Returns: JSON object mapping field names to arrays of distinct values.

You may skip this and directly call query_logs. If a query returns no results, consider then calling get_fields to validate field names or broaden the time window.
`),
		mcp.WithString("contextID", mcp.Required(), mcp.Description("Context identifier to inspect.")),
		mcp.WithString("last", mcp.Description("Optional relative time window for field discovery (e.g. 30m, 2h). Defaults to 15m.")),
	)
	getFieldsHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_, searchFactory := cm.Get()

		// Extract required parameter contextID
		contextID, err := request.RequireString("contextID")
		if err != nil || contextID == "" {
			return mcp.NewToolResultError(fmt.Sprintf("invalid or missing contextID: %v", err)), nil
		}

		// Provide a small default time window unless user overrides with last
		search := client.LogSearch{}
		if lastVal, e2 := request.RequireString("last"); e2 == nil && lastVal != "" {
			search.Range.Last.S(lastVal)
		} else if !search.Range.Last.Set && !search.Range.Gte.Set {
			search.Range.Last.S("15m")
		}

		searchResult, err := searchFactory.GetSearchResult(ctx, contextID, []string{}, search, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		fields, _, err := searchResult.GetFields(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		jsonBytes, err := json.Marshal(fields)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal fields: %v", err)), nil
		}
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}
	s.AddTool(getFieldsTool, getFieldsHandler)
	handlers["get_fields"] = getFieldsHandler

	queryLogsTool := mcp.NewTool("query_logs",
		mcp.WithDescription(`Query log entries for a context with optional filters and time window.

Usage Examples:
  - Simple field filter: query_logs contextID=prod-api fields={"level":"ERROR"}
  - Text pattern search: query_logs contextID=prod-api nativeQuery="_~=.*Exception.*"
  - Combined: query_logs contextID=prod-api nativeQuery="level=ERROR AND _~=.*timeout.*"

Parameters:
	contextID (string, required): Context identifier.
	last (string, optional): Relative duration window (e.g. 15m, 2h, 1d). Defaults to 15m.
	start_time (string, optional): Absolute start time (RFC3339).
	end_time (string, optional): Absolute end time (RFC3339).
	pageToken (string, optional): Token for pagination to fetch older logs.
	size (number, optional): Max number of log entries.

	fields (object, optional): STRUCTURED FIELD FILTERS for exact key/value matching.
		Use this when filtering by specific field values like level, service, status code.
		Example: {"level":"ERROR","service":"payment-api"}
		Note: This performs exact equality matching on structured log fields.

	nativeQuery (string, optional): ADVANCED QUERY EXPRESSION for complex filtering.
		Use this for:
		  1. Text pattern matching anywhere in logs (field-less search)
		  2. Complex logical expressions (AND/OR/NOT)
		  3. Regex patterns
		  4. Combining field filters with text search

		Query Expression Syntax:
		  - Field equality: level=ERROR, service=api
		  - Field-less search: _=substring or _~=regex
		  - Operators: =, !=, ~= (regex), !~= (not regex), >, >=, <, <=
		  - Logic: AND, OR, NOT
		  - Grouping: ( )
		  - Functions: exists(fieldname)

		Field-less Search (searching text anywhere in log messages):
		  - Substring match: _=Exception (finds logs containing "Exception")
		  - Regex match: _~=.*Exception.* (regex pattern match)
		  - Case matters: Use appropriate regex for case-insensitive: _~=(?i)exception

		Examples:
		  - Find any log with "Exception": nativeQuery="_~=.*Exception.*"
		  - Find errors with timeout: nativeQuery="level=ERROR AND _~=.*timeout.*"
		  - Complex: nativeQuery="(level=ERROR OR level=WARN) AND service=api AND _~=.*retry.*"
		  - Check field exists: nativeQuery="exists(trace_id) AND level=ERROR"

		Backend Translation:
		  The "_" field is automatically translated to backend-specific full-text fields:
		  - Splunk: _raw field
		  - OpenSearch/Elasticsearch: _all field
		  - Other backends: message field

Behavior:
	- If contextID is invalid, the response includes suggestions (no need to pre-call list_contexts).
	- If results are empty, meta.hints will recommend next actions (e.g. broaden last, call get_fields).
	- If more results are available, meta.nextPageToken will be included for pagination.

Returns: { "entries": [...], "meta": { resultCount, contextID, queryTime, hints?, nextPageToken? } }
`),
		mcp.WithString("contextID", mcp.Required(), mcp.Description("Context identifier to query.")),
		mcp.WithString("last", mcp.Description(`Relative time window like 15m, 2h, 1d.`)),
		mcp.WithString("start_time", mcp.Description("Absolute start time (RFC3339).")),
		mcp.WithString("end_time", mcp.Description("Absolute end time (RFC3339).")),
		mcp.WithString("pageToken", mcp.Description("Token for pagination to fetch older logs (returned in previous response meta).")),
		mcp.WithObject("fields", mcp.Description("Exact match key/value filters (JSON object).")),
		mcp.WithNumber("size", mcp.Description("Maximum number of log entries to return.")),
		mcp.WithString("nativeQuery", mcp.Description("Raw query in backend's native syntax (Splunk SPL, OpenSearch Lucene). Acts as base search with filters appended.")),
		mcp.WithObject("variables", mcp.Description("Runtime variables for the context (JSON object).")),
	)
	queryLogsHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cfg, searchFactory := cm.Get()
		start := time.Now()
		contextID, err := request.RequireString("contextID")
		if err != nil || contextID == "" {
			return mcp.NewToolResultError(fmt.Sprintf("invalid or missing contextID: %v", err)), nil
		}

		searchRequest := client.LogSearch{}
		if last, err := request.RequireString("last"); err == nil && last != "" {
			searchRequest.Range.Last.S(last)
		}
		if startTime, err := request.RequireString("start_time"); err == nil && startTime != "" {
			searchRequest.Range.Gte.S(startTime)
		}
		if endTime, err := request.RequireString("end_time"); err == nil && endTime != "" {
			searchRequest.Range.Lte.S(endTime)
		}
		if token, err := request.RequireString("pageToken"); err == nil && token != "" {
			searchRequest.PageToken.S(token)
		}
		if size, err := request.RequireFloat("size"); err == nil && int(size) > 0 {
			searchRequest.Size.S(int(size))
		}
		if nativeQuery, err := request.RequireString("nativeQuery"); err == nil && nativeQuery != "" {
			searchRequest.NativeQuery.S(nativeQuery)
		}

		runtimeVars := make(map[string]string)
		args := request.GetArguments()
		if args != nil {
			// Handle 'fields'
			if rawFields, ok := args["fields"]; ok && rawFields != nil {
				if fieldMap, ok := rawFields.(map[string]any); ok {
					if searchRequest.Fields == nil {
						searchRequest.Fields = ty.MS{}
					}
					for k, v := range fieldMap {
						searchRequest.Fields[k] = fmt.Sprintf("%v", v)
					}
				}
			}
			// Handle 'variables'
			if rawVars, ok := args["variables"]; ok && rawVars != nil {
				if varMap, ok := rawVars.(map[string]any); ok {
					for k, v := range varMap {
						runtimeVars[k] = fmt.Sprintf("%v", v)
					}
				}
			}
		}

		// Pre-flight check for required variables
		mergedContext, err := searchFactory.GetSearchContext(ctx, contextID, []string{}, searchRequest, runtimeVars)
		if err != nil {
			if errors.Is(err, config.ErrContextNotFound) {
				return handleContextNotFound(contextID, cfg, err), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("failed to get search context: %v", err)), nil
		}

		for name, def := range mergedContext.Search.Variables {
			if def.Required {
				if _, ok := runtimeVars[name]; !ok {
					if _, ok := os.LookupEnv(name); !ok {
						errMsg := fmt.Sprintf("Missing required variable '%s'. Please ask the user for '%s' and call the tool again.", name, def.Description)
						return mcp.NewToolResultError(errMsg), nil
					}
				}
			}
		}

		// Fallback: ensure some time window is always specified to prevent backend errors
		if !searchRequest.Range.Last.Set && !searchRequest.Range.Gte.Set {
			searchRequest.Range.Last.S("15m")
		}

		searchResult, err := searchFactory.GetSearchResult(ctx, contextID, []string{}, searchRequest, runtimeVars)
		if err != nil {
			// This logic can be simplified now as we have a pre-flight check
			return mcp.NewToolResultError(err.Error()), nil
		}

		entries, _, err := searchResult.GetEntries(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		meta := map[string]any{
			"resultCount": len(entries),
			"contextID":   contextID,
			"queryTime":   time.Since(start).String(),
		}
		if pagination := searchResult.GetPaginationInfo(); pagination != nil && pagination.NextPageToken != "" {
			meta["nextPageToken"] = pagination.NextPageToken
		}
		if len(entries) == 0 {
			meta["hints"] = []string{
				"No results: consider broadening 'last' (e.g. last=2h)",
				"If you used filters, verify field names via get_fields",
			}
		}
		response := map[string]any{"entries": entries, "meta": meta}
		jsonBytes, err := json.Marshal(response)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
		}
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}
	s.AddTool(queryLogsTool, queryLogsHandler)
	handlers["query_logs"] = queryLogsHandler

	// --- Tool: get_field_values ---
	getFieldValuesTool := mcp.NewTool("get_field_values",
		mcp.WithDescription(`Get distinct values for specific log fields to understand data distribution or find specific values.

Usage: get_field_values contextID=<context> fields=["level","error_code"] [last=15m]

Parameters:
  contextID (string, required): Context identifier.
  fields (array of strings, required): Field names to get distinct values for.
  last (string, optional): Relative time window (e.g. 15m, 2h). Defaults to 15m.
  start_time (string, optional): Absolute start time (RFC3339).
  end_time (string, optional): Absolute end time (RFC3339).
  filters (object, optional): Additional key/value filters to apply.

Returns: JSON object mapping field names to arrays of distinct values.

Example response:
{
  "level": ["ERROR", "WARN", "INFO"],
  "error_code": ["TIMEOUT", "AUTH_FAILURE", "DB_CONN_ERR"]
}
`),
		mcp.WithString("contextID", mcp.Required(), mcp.Description("Context identifier to query.")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Field names to get distinct values for (array of strings).")),
		mcp.WithString("last", mcp.Description("Relative time window like 15m, 2h, 1d.")),
		mcp.WithString("start_time", mcp.Description("Absolute start time (RFC3339).")),
		mcp.WithString("end_time", mcp.Description("Absolute end time (RFC3339).")),
		mcp.WithObject("filters", mcp.Description("Additional key/value filters to apply (JSON object).")),
		mcp.WithObject("variables", mcp.Description("Runtime variables for the context (JSON object).")),
	)
	getFieldValuesHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cfg, searchFactory := cm.Get()
		contextID, err := request.RequireString("contextID")
		if err != nil || contextID == "" {
			return mcp.NewToolResultError(fmt.Sprintf("invalid or missing contextID: %v", err)), nil
		}

		// Extract fields array
		var fieldNames []string
		args := request.GetArguments()
		if args != nil {
			if rawFields, ok := args["fields"]; ok && rawFields != nil {
				switch v := rawFields.(type) {
				case []interface{}:
					for _, f := range v {
						if s, ok := f.(string); ok {
							fieldNames = append(fieldNames, s)
						}
					}
				case []string:
					fieldNames = v
				}
			}
		}
		if len(fieldNames) == 0 {
			return mcp.NewToolResultError("fields parameter is required and must be a non-empty array of field names"), nil
		}

		searchRequest := client.LogSearch{}
		if last, err := request.RequireString("last"); err == nil && last != "" {
			searchRequest.Range.Last.S(last)
		}
		if startTime, err := request.RequireString("start_time"); err == nil && startTime != "" {
			searchRequest.Range.Gte.S(startTime)
		}
		if endTime, err := request.RequireString("end_time"); err == nil && endTime != "" {
			searchRequest.Range.Lte.S(endTime)
		}

		// Handle filters and variables
		runtimeVars := make(map[string]string)
		if args != nil {
			if rawFilters, ok := args["filters"]; ok && rawFilters != nil {
				if filterMap, ok := rawFilters.(map[string]any); ok {
					if searchRequest.Fields == nil {
						searchRequest.Fields = ty.MS{}
					}
					for k, v := range filterMap {
						searchRequest.Fields[k] = fmt.Sprintf("%v", v)
					}
				}
			}
			// Parse variables
			if rawVars, ok := args["variables"]; ok && rawVars != nil {
				if varMap, ok := rawVars.(map[string]any); ok {
					for k, v := range varMap {
						runtimeVars[k] = fmt.Sprintf("%v", v)
					}
				}
			}
		}

		// Fallback: ensure some time window is always specified
		if !searchRequest.Range.Last.Set && !searchRequest.Range.Gte.Set {
			searchRequest.Range.Last.S("15m")
		}

		// Pre-flight check for context existence
		_, err = searchFactory.GetSearchContext(ctx, contextID, []string{}, searchRequest, runtimeVars)
		if err != nil {
			if errors.Is(err, config.ErrContextNotFound) {
				return handleContextNotFound(contextID, cfg, err), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("failed to get search context: %v", err)), nil
		}

		fieldValues, err := searchFactory.GetFieldValues(ctx, contextID, []string{}, searchRequest, fieldNames, runtimeVars)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get field values: %v", err)), nil
		}

		jsonBytes, err := json.Marshal(fieldValues)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal field values: %v", err)), nil
		}
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}
	s.AddTool(getFieldValuesTool, getFieldValuesHandler)
	handlers["get_field_values"] = getFieldValuesHandler

	getContextDetailsTool := mcp.NewTool("get_context_details",
		mcp.WithDescription(`Inspect a context's configuration including required variables, backend type, and capabilities.

Usage: get_context_details contextID=<context>

Parameters:
  contextID (string, required): Context identifier to inspect.

Returns: JSON object with context configuration (backend type, variables, field mappings).

When to use:
  - Before query_logs if you need to know what variables are required
  - To discover backend-specific capabilities (e.g., native query syntax)
  - To understand field mappings and available filters for a context
`),
		mcp.WithString("contextID", mcp.Required(), mcp.Description("The context ID to inspect.")),
	)
	getContextDetailsHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cfg, searchFactory := cm.Get()
		contextID, err := request.RequireString("contextID")
		if err != nil {
			return mcp.NewToolResultError("contextID is required"), nil
		}
		searchContext, err := searchFactory.GetSearchContext(ctx, contextID, []string{}, client.LogSearch{}, nil)
		if err != nil {
			if errors.Is(err, config.ErrContextNotFound) {
				return handleContextNotFound(contextID, cfg, err), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("failed to get context details: %v", err)), nil
		}
		jsonBytes, err := json.Marshal(searchContext.Search)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal context details: %v", err)), nil
		}
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}
	s.AddTool(getContextDetailsTool, getContextDetailsHandler)
	handlers["get_context_details"] = getContextDetailsHandler

	// Resource providing context list (alternative to tool usage)
	contextsResource := mcp.NewResource(
		"logviewer://contexts",
		"LogViewer Context Index",
		mcp.WithResourceDescription("JSON array of available context IDs; server also suggests them on invalid context query."),
		mcp.WithMIMEType("application/json"),
	)
	s.AddResource(contextsResource, func(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		cfg, _ := cm.Get()
		ids := make([]string, 0, len(cfg.Contexts))
		for id := range cfg.Contexts {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		b, err := json.Marshal(ids)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal context IDs: %w", err)
		}
		return []mcp.ResourceContents{mcp.TextResourceContents{URI: "logviewer://contexts", MIMEType: "application/json", Text: string(b)}}, nil
	})

	// Register dynamic context-specific prompts
	generateContextPrompts(s, cm)

	// Prompt guiding efficient investigation workflow (generic fallback)
	investigationPrompt := mcp.NewPrompt(
		"log_investigation",
		mcp.WithPromptDescription("Guide for investigating logs: query first, broaden or discover fields only if needed."),
		mcp.WithArgument("objective", mcp.ArgumentDescription("High-level goal (e.g. detect payment errors).")),
		mcp.WithArgument("contextID", mcp.ArgumentDescription("Optional starting context.")),
	)
	s.AddPrompt(investigationPrompt, func(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cfg, _ := cm.Get()
		obj := request.Params.Arguments["objective"]
		ctxID := request.Params.Arguments["contextID"]
		if ctxID == "" {
			ids := make([]string, 0, len(cfg.Contexts))
			for id := range cfg.Contexts {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			if len(ids) > 0 {
				ctxID = ids[0]
			}
		}
		text := fmt.Sprintf(`Objective: %s
Strategy:
1. query_logs contextID=%s last=15m size=20
2. If no results: increase last (e.g. 1h) or drop filters
3. Only call get_fields if filters might be invalid or repeated empty result
4. On context error: check suggestions or resource logviewer://contexts
5. Summarize anomalies, refine with additional field filters
Return a short plan then perform tool calls.
`, obj, ctxID)
		return mcp.NewGetPromptResult("Log Investigation", []mcp.PromptMessage{mcp.NewPromptMessage(mcp.RoleAssistant, mcp.NewTextContent(text))}), nil
	})
	return &MCPServerBundle{Server: s, ToolHandlers: handlers}, nil
}

func init() {
	mcpCmd.Flags().IntVar(&mcpPort, "port", 8081, "Port for the MCP server")
	rootCmd.AddCommand(mcpCmd)
}

// handleContextNotFound creates a standardized MCP response for context not found errors.
// It includes suggestions for similar context names to help users correct typos.
func handleContextNotFound(contextID string, cfg *config.ContextConfig, err error) *mcp.CallToolResult {
	all := make([]string, 0, len(cfg.Contexts))
	for id := range cfg.Contexts {
		all = append(all, id)
	}
	sort.Strings(all)
	suggestions := suggestSimilar(contextID, all, 3)
	payload := map[string]any{
		"code":              "CONTEXT_NOT_FOUND",
		"error":             err.Error(),
		"invalidContext":    contextID,
		"availableContexts": all,
		"suggestions":       suggestions,
		"hint":              "Use a suggested contextID or call list_contexts for enumeration.",
	}
	b, mErr := json.Marshal(payload)
	if mErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal error payload: %v", mErr))
	}
	return mcp.NewToolResultText(string(b))
}

// suggestSimilar returns up to maxCount suggestions ranked by simple edit distance (Levenshtein) and substring match boost.
func suggestSimilar(target string, candidates []string, maxCount int) []string {
	type scored struct {
		v     string
		d     int
		boost bool
	}
	scoredList := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		if c == target {
			continue
		}
		boost := strings.Contains(strings.ToLower(c), strings.ToLower(target))
		scoredList = append(scoredList, scored{v: c, d: levenshtein(target, c), boost: boost})
	}
	sort.Slice(scoredList, func(i, j int) bool {
		if scoredList[i].d != scoredList[j].d {
			return scoredList[i].d < scoredList[j].d
		}
		return scoredList[i].boost && !scoredList[j].boost
	})
	out := make([]string, 0, maxCount)
	for _, s := range scoredList {
		out = append(out, s.v)
		if len(out) >= maxCount {
			break
		}
	}
	return out
}

// levenshtein computes Levenshtein distance between two strings.
func levenshtein(a, b string) int {
	r1, r2 := []rune(a), []rune(b)
	n, m := len(r1), len(r2)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}
	dp := make([]int, m+1)
	for j := 0; j <= m; j++ {
		dp[j] = j
	}
	for i := 1; i <= n; i++ {
		prev := dp[0]
		dp[0] = i
		for j := 1; j <= m; j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			ins := dp[j] + 1
			del := dp[j-1] + 1
			subst := prev + cost
			prev = dp[j]
			minVal := ins
			if del < minVal {
				minVal = del
			}
			if subst < minVal {
				minVal = subst
			}
			dp[j] = minVal
		}
	}
	return dp[m]
}

// generateContextPrompts creates and registers MCP prompts for all contexts.
func generateContextPrompts(s *server.MCPServer, cm *ConfigManager) {
	cfg, _ := cm.Get()

	for contextID, ctx := range cfg.Contexts {
		// Skip if prompt generation is disabled for this context
		if ctx.Prompt.Disabled {
			continue
		}

		// Get backend type from client config
		backendType := "unknown"
		if client, ok := cfg.Clients[ctx.Client]; ok {
			backendType = client.Type
		}

		// Build prompt description
		promptDesc := ctx.Prompt.Description
		if promptDesc == "" {
			promptDesc = fmt.Sprintf("Investigation guide for %s (%s backend)", contextID, backendType)
			if ctx.Description != "" {
				promptDesc = fmt.Sprintf("%s - %s", promptDesc, ctx.Description)
			}
		}

		// Create prompt with arguments
		prompt := mcp.NewPrompt(
			fmt.Sprintf("investigate_%s", contextID),
			mcp.WithPromptDescription(promptDesc),
			mcp.WithArgument("objective", mcp.ArgumentDescription("What you're investigating (e.g., 'find payment failures', 'trace latency issues')")),
			mcp.WithArgument("timeRange", mcp.ArgumentDescription("Time window to search (e.g., '15m', '1h', '24h'). Default: 15m")),
		)

		// Capture variables for closure
		ctxID := contextID
		ctxConfig := ctx
		backend := backendType

		// Register prompt with handler
		s.AddPrompt(prompt, func(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return generatePromptContent(cm, ctxID, ctxConfig, backend, request)
		})
	}
}

// generatePromptContent builds the full prompt text for a context.
func generatePromptContent(
	_ *ConfigManager,
	contextID string,
	ctxConfig config.SearchContext,
	backendType string,
	request mcp.GetPromptRequest,
) (*mcp.GetPromptResult, error) {
	objective := request.Params.Arguments["objective"]
	if objective == "" {
		objective = "general log investigation"
	}
	timeRange := request.Params.Arguments["timeRange"]
	if timeRange == "" {
		timeRange = "15m"
	}

	guide := backendSyntaxGuides[backendType]
	if guide.Name == "" {
		guide = BackendGuide{Name: backendType, QueryLang: "N/A"}
	}

	var sb strings.Builder

	// Section 1: Context Overview
	sb.WriteString(fmt.Sprintf("# Investigation Guide: %s\n\n", contextID))
	sb.WriteString(fmt.Sprintf("**Objective:** %s\n", objective))
	sb.WriteString(fmt.Sprintf("**Time Range:** %s\n\n", timeRange))

	sb.WriteString("## Context Overview\n")
	if ctxConfig.Description != "" {
		sb.WriteString(fmt.Sprintf("- **Description:** %s\n", ctxConfig.Description))
	}
	sb.WriteString(fmt.Sprintf("- **Backend:** %s (%s)\n", guide.Name, guide.QueryLang))
	sb.WriteString(fmt.Sprintf("- **Client:** %s\n\n", ctxConfig.Client))

	// Section 2: Variables (if any)
	if len(ctxConfig.Search.Variables) > 0 {
		sb.WriteString("## Variables\n")
		for name, def := range ctxConfig.Search.Variables {
			required := ""
			if def.Required {
				required = " **[REQUIRED]**"
			}
			defaultVal := ""
			if def.Default != nil {
				defaultVal = fmt.Sprintf(" (default: %v)", def.Default)
			}
			sb.WriteString(fmt.Sprintf("- `%s`%s: %s%s\n", name, required, def.Description, defaultVal))
		}
		sb.WriteString("\n")
	}

	// Section 3: Field Extraction Info
	fe := ctxConfig.Search.FieldExtraction
	if fe.JSON.Set && fe.JSON.Value {
		sb.WriteString("## Field Extraction\n")
		sb.WriteString("- **Format:** JSON structured logs\n")
		if fe.JSONMessageKey.Set {
			sb.WriteString(fmt.Sprintf("  - Message key: `%s`\n", fe.JSONMessageKey.Value))
		}
		if fe.JSONLevelKey.Set {
			sb.WriteString(fmt.Sprintf("  - Level key: `%s`\n", fe.JSONLevelKey.Value))
		}
		if fe.JSONTimestampKey.Set {
			sb.WriteString(fmt.Sprintf("  - Timestamp key: `%s`\n", fe.JSONTimestampKey.Value))
		}
		sb.WriteString("\n")
	}

	// Section 4: Native Query Syntax
	if guide.SyntaxGuide != "" {
		sb.WriteString("## Native Query Syntax\n")
		sb.WriteString(fmt.Sprintf("```\n%s\n```\n\n", guide.SyntaxGuide))
	}

	// Section 5: Example Queries
	examples := guide.ExampleQueries
	if len(ctxConfig.Prompt.ExampleQueries) > 0 {
		examples = ctxConfig.Prompt.ExampleQueries
	}
	if len(examples) > 0 {
		sb.WriteString("## Example Queries\n")
		for _, ex := range examples {
			sb.WriteString(fmt.Sprintf("- `%s`\n", ex))
		}
		sb.WriteString("\n")
	}

	// Section 6: Query Syntax Guide
	sb.WriteString("## Query Syntax Guide\n\n")
	sb.WriteString("### Structured Field Filtering (fields parameter)\n")
	sb.WriteString("Use for exact field value matching:\n")
	sb.WriteString("```\n")
	sb.WriteString(fmt.Sprintf("query_logs contextID=%s fields={\"level\":\"ERROR\"}\n", contextID))
	sb.WriteString(fmt.Sprintf("query_logs contextID=%s fields={\"level\":\"ERROR\",\"service\":\"api\"}\n", contextID))
	sb.WriteString("```\n\n")

	sb.WriteString("### Text Pattern Search (nativeQuery with _)\n")
	sb.WriteString("Use for finding text anywhere in log messages:\n")
	sb.WriteString("```\n")
	sb.WriteString(fmt.Sprintf("query_logs contextID=%s nativeQuery=\"_~=.*Exception.*\"\n", contextID))
	sb.WriteString(fmt.Sprintf("query_logs contextID=%s nativeQuery=\"_~=.*timeout.*\"\n", contextID))
	sb.WriteString(fmt.Sprintf("query_logs contextID=%s nativeQuery=\"_=ConnectionError\"\n", contextID))
	sb.WriteString("```\n\n")

	sb.WriteString("### Combined Filtering (nativeQuery with field filters)\n")
	sb.WriteString("Use for complex queries:\n")
	sb.WriteString("```\n")
	sb.WriteString(fmt.Sprintf("query_logs contextID=%s nativeQuery=\"level=ERROR AND _~=.*Exception.*\"\n", contextID))
	sb.WriteString(fmt.Sprintf("query_logs contextID=%s nativeQuery=\"(level=ERROR OR level=WARN) AND _~=.*retry.*\"\n", contextID))
	sb.WriteString("```\n\n")

	// Section 7: Investigation Workflow
	sb.WriteString("## Investigation Workflow\n")
	sb.WriteString(fmt.Sprintf(`1. **Start broad:** query_logs contextID=%s last=%s size=50
2. **Review results:** Look for patterns, errors, anomalies
3. **Narrow down:** Add filters (e.g., fields={"level":"ERROR"})
4. **Use native query:** For complex searches, use nativeQuery parameter
5. **Discover fields:** If unsure about field names, call get_fields contextID=%s

`, contextID, timeRange, contextID))

	// Section 8: Quick Start Command
	sb.WriteString("## Quick Start\n")
	sb.WriteString("```\n")
	sb.WriteString(fmt.Sprintf("query_logs contextID=%s last=%s", contextID, timeRange))

	// Add any required variables to the example as a single JSON object
	var requiredVarParts []string
	for name, def := range ctxConfig.Search.Variables {
		if def.Required {
			exampleVal := "<value>"
			if def.Default != nil {
				exampleVal = fmt.Sprintf("%v", def.Default)
			}
			requiredVarParts = append(requiredVarParts, fmt.Sprintf("\"%s\":\"%s\"", name, exampleVal))
		}
	}
	if len(requiredVarParts) > 0 {
		sb.WriteString(fmt.Sprintf(" variables={%s}", strings.Join(requiredVarParts, ", ")))
	}
	sb.WriteString("\n```\n")

	return mcp.NewGetPromptResult(
		fmt.Sprintf("Investigation Guide: %s", contextID),
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleAssistant, mcp.NewTextContent(sb.String())),
		},
	), nil
}
