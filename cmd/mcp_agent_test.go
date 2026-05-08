package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/ty"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// =============================================================================
// Ollama LLM Client for MCP Testing
// =============================================================================

// OllamaClient interfaces with a local Ollama instance for LLM-driven testing.
type OllamaClient struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

// OllamaChatRequest represents a request to Ollama's chat API.
type OllamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Tools    []OllamaTool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
	Options  OllamaOptions   `json:"options,omitempty"`
}

// OllamaOptions for controlling model behavior.
type OllamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	Seed        int     `json:"seed,omitempty"`
}

// OllamaMessage represents a chat message.
type OllamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []OllamaToolCall `json:"tool_calls,omitempty"`
}

// OllamaToolCall represents a tool invocation by the model.
type OllamaToolCall struct {
	ID       string             `json:"id,omitempty"`
	Function OllamaFunctionCall `json:"function"`
}

// OllamaFunctionCall contains the function name and arguments.
type OllamaFunctionCall struct {
	Index     int                    `json:"index,omitempty"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// OllamaTool represents a tool definition for Ollama.
type OllamaTool struct {
	Type     string             `json:"type"`
	Function OllamaToolFunction `json:"function"`
}

// OllamaToolFunction describes a function/tool.
type OllamaToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// OllamaChatResponse represents Ollama's response.
type OllamaChatResponse struct {
	Model      string        `json:"model"`
	Message    OllamaMessage `json:"message"`
	Done       bool          `json:"done"`
	DoneReason string        `json:"done_reason,omitempty"`
}

// NewOllamaClient creates a new Ollama client.
func NewOllamaClient(baseURL, model string) *OllamaClient {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "llama3.1" // Recommended for good tool-calling support
	}
	return &OllamaClient{
		BaseURL: baseURL,
		Model:   model,
		Client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// IsAvailable checks if Ollama is running and the model is available.
func (o *OllamaClient) IsAvailable() bool {
	resp, err := o.Client.Get(o.BaseURL + "/api/tags")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == 200
}

// Chat sends a message to Ollama with optional tools.
func (o *OllamaClient) Chat(ctx context.Context, messages []OllamaMessage, tools []OllamaTool) (*OllamaChatResponse, error) {
	req := OllamaChatRequest{
		Model:    o.Model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
		Options: OllamaOptions{
			Temperature: 0.1, // Low for deterministic tool calling
			Seed:        42,  // Reproducibility
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp OllamaChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatResp, nil
}

// ChatDebug is like Chat but returns the raw response for debugging.
func (o *OllamaClient) ChatDebug(ctx context.Context, messages []OllamaMessage, tools []OllamaTool) (*OllamaChatResponse, string, error) {
	req := OllamaChatRequest{
		Model:    o.Model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
		Options: OllamaOptions{
			Temperature: 0.1,
			Seed:        42,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.Client.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, string(respBody), fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var chatResp OllamaChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, string(respBody), fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatResp, string(respBody), nil
}

// =============================================================================
// MCP Agent Test Harness
// =============================================================================

// MCPAgentTestHarness connects an LLM to an MCP server for testing.
type MCPAgentTestHarness struct {
	MCPClient    *mcpclient.Client
	OllamaClient *OllamaClient
	Tools        []mcp.Tool
	MaxTurns     int
}

// AgentTestResult captures the outcome of an agent test.
type AgentTestResult struct {
	FinalResponse    string
	ToolCallSequence []ToolCallRecord
	TotalTurns       int
	Success          bool
	Error            error
}

// ToolCallRecord records a single tool invocation.
type ToolCallRecord struct {
	ToolName  string
	Arguments map[string]interface{}
	Result    string
	IsError   bool
}

// NewMCPAgentTestHarness creates a test harness with the given MCP server and Ollama client.
func NewMCPAgentTestHarness(mcpServer *MCPServerBundle, ollamaClient *OllamaClient) (*MCPAgentTestHarness, error) {
	// Create in-process MCP client
	mcpClient, err := mcpclient.NewInProcessClient(mcpServer.Server)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start the transport
	if err := mcpClient.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start MCP client: %w", err)
	}

	// Initialize the MCP protocol
	_, err = mcpClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo: mcp.Implementation{
				Name:    "logviewer-test-agent",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
	}

	// Fetch available tools
	toolsResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	return &MCPAgentTestHarness{
		MCPClient:    mcpClient,
		OllamaClient: ollamaClient,
		Tools:        toolsResult.Tools,
		MaxTurns:     10,
	}, nil
}

// ConvertToolsToOllamaFormat converts MCP tools to Ollama's tool format.
// It simplifies complex schemas to work better with smaller models.
func (h *MCPAgentTestHarness) ConvertToolsToOllamaFormat() []OllamaTool {
	tools := make([]OllamaTool, 0, len(h.Tools))
	for _, tool := range h.Tools {
		// Simplify the schema for better LLM compatibility
		params := simplifySchema(tool.InputSchema)

		tools = append(tools, OllamaTool{
			Type: "function",
			Function: OllamaToolFunction{
				Name:        tool.Name,
				Description: getShortDescription(tool.Description),
				Parameters:  params,
			},
		})
	}
	return tools
}

// simplifySchema converts complex MCP schemas to simpler ones for LLM consumption.
func simplifySchema(schema mcp.ToolInputSchema) map[string]interface{} {
	result := map[string]interface{}{
		"type": "object",
	}

	if schema.Properties != nil {
		props := make(map[string]interface{})
		for name, prop := range schema.Properties {
			// Extract just the type and description
			propMap := make(map[string]interface{})
			if propObj, ok := prop.(map[string]interface{}); ok {
				if t, exists := propObj["type"]; exists {
					propMap["type"] = t
				} else {
					propMap["type"] = "string"
				}
				if desc, exists := propObj["description"]; exists {
					propMap["description"] = desc
				}
			} else {
				propMap["type"] = "string"
			}
			props[name] = propMap
		}
		result["properties"] = props
	}

	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}

	return result
}

// getShortDescription returns a shortened description (first sentence or 100 chars).
func getShortDescription(desc string) string {
	// Take first line only
	if idx := strings.Index(desc, "\n"); idx > 0 {
		desc = desc[:idx]
	}
	// Limit length
	if len(desc) > 150 {
		desc = desc[:147] + "..."
	}
	return strings.TrimSpace(desc)
}

// RunAgentLoop executes an agentic loop where the LLM can call MCP tools.
func (h *MCPAgentTestHarness) RunAgentLoop(ctx context.Context, userPrompt string) *AgentTestResult {
	result := &AgentTestResult{
		ToolCallSequence: []ToolCallRecord{},
	}

	// Build system prompt - be very explicit about tool usage
	systemPrompt := `You are a log investigation assistant. You MUST use the provided tools to complete tasks.

IMPORTANT RULES:
1. DO NOT describe what tools to use - actually CALL them
2. When asked to find logs, you MUST call query_logs or list_contexts
3. Never explain how to use a tool - just use it directly
4. After getting tool results, summarize what you found

QUERY CONSTRUCTION GUIDELINES:

When calling query_logs, choose the right parameter:

A. Use "fields" parameter for STRUCTURED FILTERING:
   - When filtering by specific field values (level, service, status, etc.)
   - Example: fields={"level":"ERROR"}
   - Example: fields={"level":"ERROR","service":"payment-api"}

B. Use "nativeQuery" parameter for TEXT PATTERN MATCHING:
   - When searching for text/patterns anywhere in log messages
   - When user asks to "find", "look for", "search for" specific text/errors/exceptions
   - Syntax: _~=.*pattern.* for regex match, _=text for substring

   Examples:
   - Find "Exception": nativeQuery="_~=.*Exception.*"
   - Find "timeout": nativeQuery="_~=.*timeout.*"
   - Find "NullPointerException": nativeQuery="_~=.*NullPointerException.*"

C. Use "nativeQuery" for COMPLEX QUERIES:
   - Combine field filters with text search
   - Use logical operators (AND, OR, NOT)

   Examples:
   - Error logs with "Exception": nativeQuery="level=ERROR AND _~=.*Exception.*"
   - Errors or warnings with "retry": nativeQuery="(level=ERROR OR level=WARN) AND _~=.*retry.*"

KEY PATTERNS:
  - "Find all Exception" → nativeQuery="_~=.*Exception.*"
  - "Show ERROR logs" → fields={"level":"ERROR"}
  - "Find errors with timeout" → nativeQuery="level=ERROR AND _~=.*timeout.*"

You have access to these tools:
- list_contexts: List available log contexts
- query_logs: Query logs from a context with filters
- get_fields: Get available field names for a context
- get_field_values: Get distinct values for fields
- get_context_details: Get context configuration

Always execute tool calls to complete the user's request.`

	messages := []OllamaMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	ollamaTools := h.ConvertToolsToOllamaFormat()

	// Debug: log tools being sent on first call
	if len(ollamaTools) > 0 {
		toolsJSON, _ := json.MarshalIndent(ollamaTools[:1], "", "  ") // Just first tool
		_ = toolsJSON                                                 // Avoid unused variable in non-debug builds
	}

	for turn := 0; turn < h.MaxTurns; turn++ {
		result.TotalTurns = turn + 1

		// Call Ollama with debug
		resp, rawResp, err := h.OllamaClient.ChatDebug(ctx, messages, ollamaTools)
		_ = rawResp // Can enable for debugging
		if err != nil {
			result.Error = fmt.Errorf("turn %d: ollama chat failed: %w", turn, err)
			return result
		}

		// Check for tool calls
		if len(resp.Message.ToolCalls) > 0 {
			// Process each tool call
			for _, toolCall := range resp.Message.ToolCalls {
				// Arguments are already parsed as map[string]interface{}
				args := toolCall.Function.Arguments

				// Call the MCP tool
				toolResult, toolErr := h.MCPClient.CallTool(ctx, mcp.CallToolRequest{
					Params: mcp.CallToolParams{
						Name:      toolCall.Function.Name,
						Arguments: args,
					},
				})

				// Record the tool call
				record := ToolCallRecord{
					ToolName:  toolCall.Function.Name,
					Arguments: args,
				}

				if toolErr != nil {
					record.IsError = true
					record.Result = toolErr.Error()
				} else if len(toolResult.Content) > 0 {
					// Extract text content
					for _, content := range toolResult.Content {
						if textContent, ok := content.(mcp.TextContent); ok {
							record.Result = textContent.Text
							break
						}
					}
				}
				result.ToolCallSequence = append(result.ToolCallSequence, record)

				// Add tool result to conversation
				messages = append(messages, OllamaMessage{
					Role:      "assistant",
					Content:   "",
					ToolCalls: resp.Message.ToolCalls,
				})
				messages = append(messages, OllamaMessage{
					Role:    "tool",
					Content: record.Result,
				})
			}
		} else {
			// No tool calls - agent is done
			result.FinalResponse = resp.Message.Content
			result.Success = true
			return result
		}
	}

	result.Error = fmt.Errorf("exceeded max turns (%d)", h.MaxTurns)
	return result
}

// Close cleans up the harness resources.
func (h *MCPAgentTestHarness) Close() error {
	if h.MCPClient != nil {
		return h.MCPClient.Close()
	}
	return nil
}

// =============================================================================
// Test Helpers
// =============================================================================

// createTestConfig creates a minimal config for testing.
func createTestConfig() *config.ContextConfig {
	cfg := &config.ContextConfig{
		Clients:  config.Clients{},
		Searches: config.Searches{},
		Contexts: config.Contexts{},
	}

	// Add local client
	cfg.Clients["test-local"] = config.Client{Type: "local", Options: ty.MI{}}

	// Add JSON extraction search
	jsonSearch := client.LogSearch{}
	jsonSearch.FieldExtraction.JSON.S(true)
	jsonSearch.FieldExtraction.JSONLevelKey.S("level")
	jsonSearch.FieldExtraction.JSONMessageKey.S("message")
	jsonSearch.FieldExtraction.JSONTimestampKey.S("@timestamp")
	cfg.Searches["json-extraction"] = jsonSearch

	// Add test contexts
	cfg.Contexts["payment-service"] = config.SearchContext{
		Description:   "Payment Service logs for testing",
		Client:        "test-local",
		SearchInherit: []string{"json-extraction"},
		Search:        client.LogSearch{},
	}

	cfg.Contexts["order-service"] = config.SearchContext{
		Description:   "Order Service logs for testing",
		Client:        "test-local",
		SearchInherit: []string{"json-extraction"},
		Search:        client.LogSearch{},
	}

	cfg.Contexts["api-gateway"] = config.SearchContext{
		Description: "API Gateway logs for testing",
		Client:      "test-local",
		Search:      client.LogSearch{},
	}

	return cfg
}

// skipIfNoOllama skips the test if Ollama is not available.
func skipIfNoOllama(t *testing.T) *OllamaClient {
	t.Helper()

	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "http://localhost:11434"
	}

	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		// Use llama3.1 or qwen2.5 - better tool calling support than mistral
		model = "llama3.1"
	}

	client := NewOllamaClient(host, model)
	if !client.IsAvailable() {
		t.Skipf("Ollama not available at %s (set OLLAMA_HOST or run 'ollama serve')", host)
	}

	t.Logf("Using Ollama model: %s", model)
	return client
}

// =============================================================================
// Integration Tests
// =============================================================================

// TestMCPAgent_DiscoveryWorkflow tests the agent's ability to discover contexts and fields.
func TestMCPAgent_DiscoveryWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LLM integration test in short mode")
	}

	ollama := skipIfNoOllama(t)

	// Setup MCP server
	cfg := createTestConfig()
	cm, err := NewConfigManagerForTest(cfg)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	bundle, err := buildMCPServerWithManager(cm)
	if err != nil {
		t.Fatalf("failed to build MCP server: %v", err)
	}

	// Create test harness
	harness, err := NewMCPAgentTestHarness(bundle, ollama)
	if err != nil {
		t.Fatalf("failed to create test harness: %v", err)
	}
	defer func() { _ = harness.Close() }()

	// Run the test
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result := harness.RunAgentLoop(ctx, "What log contexts are available? List them for me.")

	// Validate results
	if result.Error != nil {
		t.Fatalf("agent loop failed: %v", result.Error)
	}

	// Check that list_contexts was called
	foundListContexts := false
	for _, call := range result.ToolCallSequence {
		if call.ToolName == "list_contexts" {
			foundListContexts = true
			// Verify the result contains expected contexts
			if !strings.Contains(call.Result, "payment-service") {
				t.Errorf("list_contexts should return payment-service, got: %s", call.Result)
			}
		}
	}

	t.Logf("Agent completed in %d turns", result.TotalTurns)
	t.Logf("Tool calls made: %d", len(result.ToolCallSequence))
	t.Logf("Final response: %s", result.FinalResponse)

	if !foundListContexts {
		// This may happen with smaller models that describe instead of call
		t.Logf("NOTE: Model did not call tools. Try a larger model: OLLAMA_MODEL=llama3.1:70b")
		t.Errorf("expected agent to call list_contexts, tool calls: %+v", result.ToolCallSequence)
	}
}

// TestMCPAgent_ErrorInvestigation tests the agent's ability to find error logs.
func TestMCPAgent_ErrorInvestigation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LLM integration test in short mode")
	}

	ollama := skipIfNoOllama(t)

	cfg := createTestConfig()
	cm, err := NewConfigManagerForTest(cfg)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	bundle, err := buildMCPServerWithManager(cm)
	if err != nil {
		t.Fatalf("failed to build MCP server: %v", err)
	}

	harness, err := NewMCPAgentTestHarness(bundle, ollama)
	if err != nil {
		t.Fatalf("failed to create test harness: %v", err)
	}
	defer func() { _ = harness.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result := harness.RunAgentLoop(ctx, "Find ERROR level logs in the payment-service context from the last 15 minutes.")

	if result.Error != nil {
		t.Fatalf("agent loop failed: %v", result.Error)
	}

	// Check that query_logs was called with appropriate parameters
	foundQueryLogs := false
	for _, call := range result.ToolCallSequence {
		if call.ToolName == "query_logs" {
			foundQueryLogs = true
			// Check contextId
			if ctxID, ok := call.Arguments["contextId"].(string); ok {
				if ctxID != "payment-service" {
					t.Errorf("expected contextId=payment-service, got: %s", ctxID)
				}
			}
			t.Logf("query_logs called with args: %+v", call.Arguments)
		}
	}

	if !foundQueryLogs {
		t.Errorf("expected agent to call query_logs, tool calls: %+v", result.ToolCallSequence)
	}

	t.Logf("Agent completed in %d turns", result.TotalTurns)
	t.Logf("Final response: %s", result.FinalResponse)
}

// TestMCPAgent_TextPatternSearch tests the agent's ability to use field-less search for text pattern matching.
func TestMCPAgent_TextPatternSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LLM integration test in short mode")
	}

	ollama := skipIfNoOllama(t)

	cfg := createTestConfig()
	cm, err := NewConfigManagerForTest(cfg)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	bundle, err := buildMCPServerWithManager(cm)
	if err != nil {
		t.Fatalf("failed to build MCP server: %v", err)
	}

	harness, err := NewMCPAgentTestHarness(bundle, ollama)
	if err != nil {
		t.Fatalf("failed to create test harness: %v", err)
	}
	defer func() { _ = harness.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Test the exact scenario from the user's issue
	result := harness.RunAgentLoop(ctx, "Look at all Exception in the payment-service context")

	if result.Error != nil {
		t.Fatalf("agent loop failed: %v", result.Error)
	}

	// Verify that query_logs was called with nativeQuery parameter
	foundCorrectQuery := false
	usedFieldsIncorrectly := false
	for _, call := range result.ToolCallSequence {
		if call.ToolName == "query_logs" {
			t.Logf("query_logs called with args: %+v", call.Arguments)

			// Check if nativeQuery is used (correct approach)
			if nq, ok := call.Arguments["nativeQuery"].(string); ok {
				if (strings.Contains(nq, "_=") || strings.Contains(nq, "_~=")) && strings.Contains(strings.ToLower(nq), "exception") {
					foundCorrectQuery = true
					t.Logf("SUCCESS: Agent correctly used nativeQuery with field-less search: %s", nq)
				}
			}

			// Check if fields is used incorrectly (checking for level=ERROR when searching for Exception)
			if fields, ok := call.Arguments["fields"]; ok {
				if fieldMap, ok := fields.(map[string]interface{}); ok {
					if level, exists := fieldMap["level"]; exists && level == "ERROR" {
						// If only using fields={"level":"ERROR"} without nativeQuery for text search
						if _, hasNativeQuery := call.Arguments["nativeQuery"]; !hasNativeQuery {
							usedFieldsIncorrectly = true
							t.Logf("WARNING: Agent used only fields parameter with level=%v instead of nativeQuery for text pattern", level)
						}
					}
				}
			}
		}
	}

	if !foundCorrectQuery {
		t.Errorf("Expected agent to use nativeQuery with field-less search (_~=.*Exception.* or _=Exception), but it didn't")
		t.Logf("Tool call sequence: %+v", result.ToolCallSequence)
	}

	if usedFieldsIncorrectly {
		t.Errorf("Agent incorrectly used only fields parameter for text pattern search instead of nativeQuery")
	}

	t.Logf("Agent completed in %d turns", result.TotalTurns)
	t.Logf("Final response: %s", result.FinalResponse)
}

// TestMCPAgent_MultiStepReasoning tests complex queries requiring multiple tool calls.
func TestMCPAgent_MultiStepReasoning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LLM integration test in short mode")
	}

	ollama := skipIfNoOllama(t)

	cfg := createTestConfig()
	cm, err := NewConfigManagerForTest(cfg)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	bundle, err := buildMCPServerWithManager(cm)
	if err != nil {
		t.Fatalf("failed to build MCP server: %v", err)
	}

	harness, err := NewMCPAgentTestHarness(bundle, ollama)
	if err != nil {
		t.Fatalf("failed to create test harness: %v", err)
	}
	defer func() { _ = harness.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	result := harness.RunAgentLoop(ctx, `I need to investigate an issue in the payment-service.
First, discover what fields are available, then find any ERROR logs.
Summarize what you find.`)

	if result.Error != nil {
		t.Fatalf("agent loop failed: %v", result.Error)
	}

	// Log all tool calls for debugging
	t.Logf("Tool call sequence (%d calls):", len(result.ToolCallSequence))
	for i, call := range result.ToolCallSequence {
		t.Logf("  %d. %s(%+v)", i+1, call.ToolName, call.Arguments)
	}

	t.Logf("Agent completed in %d turns", result.TotalTurns)
	t.Logf("Final response: %s", result.FinalResponse)

	// Should call multiple tools
	if len(result.ToolCallSequence) < 2 {
		t.Logf("NOTE: Model made fewer tool calls than expected. Try a larger model: OLLAMA_MODEL=llama3.1:70b")
		t.Errorf("expected at least 2 tool calls for multi-step reasoning, got: %d", len(result.ToolCallSequence))
	}
}

// TestMCPAgent_ContextNotFound tests error handling when context doesn't exist.
func TestMCPAgent_ContextNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LLM integration test in short mode")
	}

	ollama := skipIfNoOllama(t)

	cfg := createTestConfig()
	cm, err := NewConfigManagerForTest(cfg)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	bundle, err := buildMCPServerWithManager(cm)
	if err != nil {
		t.Fatalf("failed to build MCP server: %v", err)
	}

	harness, err := NewMCPAgentTestHarness(bundle, ollama)
	if err != nil {
		t.Fatalf("failed to create test harness: %v", err)
	}
	defer func() { _ = harness.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result := harness.RunAgentLoop(ctx, "Query logs from the 'nonexistent-service' context.")

	if result.Error != nil {
		t.Fatalf("agent loop failed: %v", result.Error)
	}

	// Check that the agent received error feedback and recovered
	foundError := false
	for _, call := range result.ToolCallSequence {
		if strings.Contains(call.Result, "CONTEXT_NOT_FOUND") || strings.Contains(call.Result, "suggestions") {
			foundError = true
			t.Logf("Agent received context not found error with suggestions")
		}
	}

	if !foundError {
		t.Logf("Note: Agent may have used list_contexts first to avoid error")
	}

	t.Logf("Agent completed in %d turns", result.TotalTurns)
	t.Logf("Final response: %s", result.FinalResponse)
}

// TestMCPAgent_DynamicPrompts tests the new dynamic prompt feature.
func TestMCPAgent_DynamicPrompts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LLM integration test in short mode")
	}

	ollama := skipIfNoOllama(t)

	cfg := createTestConfig()
	cm, err := NewConfigManagerForTest(cfg)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	bundle, err := buildMCPServerWithManager(cm)
	if err != nil {
		t.Fatalf("failed to build MCP server: %v", err)
	}

	// Verify prompts were generated
	harness, err := NewMCPAgentTestHarness(bundle, ollama)
	if err != nil {
		t.Fatalf("failed to create test harness: %v", err)
	}
	defer func() { _ = harness.Close() }()

	// List prompts via MCP client
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	promptsResult, err := harness.MCPClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
	if err != nil {
		t.Fatalf("failed to list prompts: %v", err)
	}

	// Check that dynamic prompts exist
	foundPaymentPrompt := false
	foundOrderPrompt := false
	for _, prompt := range promptsResult.Prompts {
		t.Logf("Found prompt: %s - %s", prompt.Name, prompt.Description)
		if prompt.Name == "investigate_payment-service" {
			foundPaymentPrompt = true
		}
		if prompt.Name == "investigate_order-service" {
			foundOrderPrompt = true
		}
	}

	if !foundPaymentPrompt {
		t.Error("expected to find investigate_payment-service prompt")
	}
	if !foundOrderPrompt {
		t.Error("expected to find investigate_order-service prompt")
	}
}

// TestMCPAgent_SimpleToolCall tests a simple direct tool call scenario.
func TestMCPAgent_SimpleToolCall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LLM integration test in short mode")
	}

	ollama := skipIfNoOllama(t)

	cfg := createTestConfig()
	cm, err := NewConfigManagerForTest(cfg)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	bundle, err := buildMCPServerWithManager(cm)
	if err != nil {
		t.Fatalf("failed to build MCP server: %v", err)
	}

	harness, err := NewMCPAgentTestHarness(bundle, ollama)
	if err != nil {
		t.Fatalf("failed to create test harness: %v", err)
	}
	defer func() { _ = harness.Close() }()

	// Use only list_contexts tool - simple with no required params
	simpleTool := OllamaTool{
		Type: "function",
		Function: OllamaToolFunction{
			Name:        "list_contexts",
			Description: "List all available log contexts. Call this to see what contexts you can query.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	messages := []OllamaMessage{
		{Role: "system", Content: "You have access to tools. When asked about log contexts, you MUST call the list_contexts tool. Do not describe what you would do - actually call the tool."},
		{Role: "user", Content: "What log contexts are available?"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, rawResp, err := ollama.ChatDebug(ctx, messages, []OllamaTool{simpleTool})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	t.Logf("Raw response: %s", rawResp)

	if len(resp.Message.ToolCalls) == 0 {
		t.Errorf("Expected tool call but got text response: %s", resp.Message.Content)
	} else {
		t.Logf("SUCCESS: Tool called: %s", resp.Message.ToolCalls[0].Function.Name)

		// Now actually execute via MCP
		result, err := harness.MCPClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "list_contexts",
			},
		})
		if err != nil {
			t.Fatalf("MCP tool call failed: %v", err)
		}
		t.Logf("MCP result: %+v", result)
	}
}

// TestOllamaToolCalling is a debug test to verify Ollama tool calling works.
func TestOllamaToolCalling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LLM integration test in short mode")
	}

	ollama := skipIfNoOllama(t)

	// Define a simple tool
	tools := []OllamaTool{
		{
			Type: "function",
			Function: OllamaToolFunction{
				Name:        "get_weather",
				Description: "Get the current weather for a location",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "The city name",
						},
					},
					"required": []string{"location"},
				},
			},
		},
	}

	messages := []OllamaMessage{
		{Role: "system", Content: "You are a helpful assistant. Use the provided tools to answer questions."},
		{Role: "user", Content: "What is the weather in Paris?"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, rawResp, err := ollama.ChatDebug(ctx, messages, tools)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	t.Logf("Raw response: %s", rawResp)
	t.Logf("Parsed response - Content: %q", resp.Message.Content)
	t.Logf("Parsed response - ToolCalls: %+v", resp.Message.ToolCalls)
	t.Logf("Done reason: %s", resp.DoneReason)

	if len(resp.Message.ToolCalls) == 0 {
		t.Logf("WARNING: Model did not call tools. This may be a model capability issue.")
		t.Logf("Try: OLLAMA_MODEL=llama3.1 or OLLAMA_MODEL=qwen2.5-coder")
	} else {
		t.Logf("Tool was called: %s", resp.Message.ToolCalls[0].Function.Name)
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

// BenchmarkMCPToolCall benchmarks direct MCP tool calls (no LLM).
func BenchmarkMCPToolCall(b *testing.B) {
	cfg := createTestConfig()
	cm, err := NewConfigManagerForTest(cfg)
	if err != nil {
		b.Fatalf("failed to create config manager: %v", err)
	}

	bundle, err := buildMCPServerWithManager(cm)
	if err != nil {
		b.Fatalf("failed to build MCP server: %v", err)
	}

	mcpClient, err := mcpclient.NewInProcessClient(bundle.Server)
	if err != nil {
		b.Fatalf("failed to create MCP client: %v", err)
	}
	defer func() { _ = mcpClient.Close() }()

	ctx := context.Background()
	if err := mcpClient.Start(ctx); err != nil {
		b.Fatalf("failed to start MCP client: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "list_contexts",
			},
		})
		if err != nil {
			b.Fatalf("tool call failed: %v", err)
		}
	}
}
