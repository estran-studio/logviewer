package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

func TestRunQueryValues(t *testing.T) {
	mockClient := &client.MockLogClient{
		OnValues: func(search client.LogSearch, field string) ([]string, error) {
			if field == "level" {
				return []string{"INFO", "ERROR"}, nil
			}
			if field == "app" {
				return []string{"frontend", "backend"}, nil
			}
			return []string{}, nil
		},
	}

	search := client.LogSearch{
		Fields: ty.MS{"env": "prod"},
	}

	t.Run("text output", func(t *testing.T) {
		var buf bytes.Buffer
		err := RunQueryValues(&buf, mockClient, search, []string{"level"}, false)
		assert.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "level")
		assert.Contains(t, output, "INFO")
		assert.Contains(t, output, "ERROR")
	})

	t.Run("json output", func(t *testing.T) {
		var buf bytes.Buffer
		err := RunQueryValues(&buf, mockClient, search, []string{"app"}, true)
		assert.NoError(t, err)

		var result map[string][]string
		err = json.Unmarshal(buf.Bytes(), &result)
		assert.NoError(t, err)

		assert.Contains(t, result, "app")
		assert.Equal(t, []string{"frontend", "backend"}, result["app"])
	})

	t.Run("multiple fields", func(t *testing.T) {
		var buf bytes.Buffer
		err := RunQueryValues(&buf, mockClient, search, []string{"level", "app"}, false)
		assert.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "level")
		assert.Contains(t, output, "app")
	})
}

func TestMergeFilterWithAnd(t *testing.T) {
	var existing *client.Filter
	added := &client.Filter{Field: "f1", Value: "v1"}

	mergeFilterWithAnd(&existing, added)
	assert.NotNil(t, existing)
	assert.Equal(t, "f1", existing.Field)

	added2 := &client.Filter{Field: "f2", Value: "v2"}
	mergeFilterWithAnd(&existing, added2)
	assert.Equal(t, client.LogicAnd, existing.Logic)
	assert.Len(t, existing.Filters, 2)
}

func TestStringArrayEnvVariable(t *testing.T) {
	maps := ty.MS{}
	err := stringArrayEnvVariable([]string{"k1=v1", "k2=v2", "free", "=val"}, &maps)
	assert.NoError(t, err)
	assert.Equal(t, "v1", maps["k1"])
	assert.Equal(t, "v2", maps["k2"])
	assert.Equal(t, "free val", maps[""])
}

func TestParseFlags(t *testing.T) {
	req := &client.LogSearch{
		Fields:          ty.MS{},
		FieldsCondition: ty.MS{},
	}
	
	// parseBasicFlags
	size = 10
	pageToken = "token"
	duration = "5s"
	refresh = true
	parseBasicFlags(req)
	assert.Equal(t, 10, req.Size.Value)
	assert.Equal(t, "token", req.PageToken.Value)
	assert.Equal(t, "5s", req.Refresh.Duration.Value)
	assert.True(t, req.Follow)

	// parseTimeFlags
	from = "2023-01-01"
	to = "2023-01-02"
	last = "1h"
	parseTimeFlags(req)
	assert.True(t, req.Range.Gte.Set)
	assert.True(t, req.Range.Lte.Set)
	assert.Equal(t, "1h", req.Range.Last.Value)

	// parseFieldFlags
	fields = []string{"level=ERROR", "msg~=err.*"}
	parseFieldFlags(req)
	assert.Equal(t, "ERROR", req.Fields["level"])
	assert.NotNil(t, req.Filter)
}

func TestParseRuntimeVars(t *testing.T) {
	vars = []string{"k1=v1", "k2=v2"}
	defer func() { vars = nil }()
	
	res := parseRuntimeVars()
	assert.Equal(t, "v1", res["k1"])
	assert.Equal(t, "v2", res["k2"])
}

func TestResolveContextIDsFromConfig(t *testing.T) {
	cfg := &config.ContextConfig{
		CurrentContext: "ctx1",
		Contexts: config.Contexts{
			"ctx1": config.SearchContext{},
		},
	}
	
	// Default
	res := resolveContextIDsFromConfig(cfg)
	assert.Equal(t, []string{"ctx1"}, res)
	
	// Override
	contextIDs = []string{"ctx2"}
	defer func() { contextIDs = nil }()
	res2 := resolveContextIDsFromConfig(cfg)
	assert.Equal(t, []string{"ctx2"}, res2)
}


func TestRunQueryField(t *testing.T) {
	mockClient := &client.MockLogClient{
		OnFields: func(search client.LogSearch) (map[string][]string, error) {
			return map[string][]string{
				"level":   {"INFO", "WARN"},
				"message": {"foo bar"},
			}, nil
		},
	}

	search := client.LogSearch{}

	t.Run("displays fields and examples", func(t *testing.T) {
		var buf bytes.Buffer
		err := RunQueryField(&buf, mockClient, search, false)
		assert.NoError(t, err)

		output := buf.String()
		// Check for field name
		assert.True(t, strings.Contains(output, "level") || strings.Contains(output, "level "), "output should contain 'level'")
		// Check for example value
		assert.True(t, strings.Contains(output, "INFO") || strings.Contains(output, "INFO\n"), "output should contain 'INFO'")
		assert.Contains(t, output, "message")
		assert.Contains(t, output, "foo bar")
	})

	t.Run("outputs JSON when requested", func(t *testing.T) {
		var buf bytes.Buffer
		err := RunQueryField(&buf, mockClient, search, true)
		assert.NoError(t, err)

		// Verify valid JSON
		var fields map[string][]string
		err = json.Unmarshal(buf.Bytes(), &fields)
		assert.NoError(t, err, "output should be valid JSON")

		// Verify content
		assert.Equal(t, []string{"INFO", "WARN"}, fields["level"])
		assert.Equal(t, []string{"foo bar"}, fields["message"])
	})
}
