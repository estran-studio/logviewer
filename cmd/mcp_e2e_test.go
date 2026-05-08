package cmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestMCP_ListContexts(t *testing.T) {
	cfg := &config.ContextConfig{Clients: config.Clients{}, Searches: config.Searches{}, Contexts: config.Contexts{}}
	cfg.Clients["dummy"] = config.Client{Type: "local", Options: ty.MI{}}
	cfg.Contexts["alpha"] = config.SearchContext{Client: "dummy", Search: client.LogSearch{}}

	cm, err := NewConfigManagerForTest(cfg)
	if err != nil {
		t.Fatalf("config manager error: %v", err)
	}
	bundle, err := buildMCPServerWithManager(cm)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	handler := bundle.ToolHandlers["list_contexts"]
	res, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("no content")
	}
	textPayload := ""
	if tc, ok := res.Content[0].(mcp.TextContent); ok {
		textPayload = tc.Text
	} else {
		b, err := json.Marshal(res.Content[0])
		if err != nil {
			t.Fatalf("failed to marshal tool content: %v", err)
		}
		textPayload = string(b)
	}
	var list []string
	if err := json.Unmarshal([]byte(textPayload), &list); err != nil {
		t.Fatalf("failed to unmarshal context list: %v raw=%s", err, textPayload)
	}
	found := false
	for _, v := range list {
		if v == "alpha" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'alpha' in context list: %v", list)
	}
}
