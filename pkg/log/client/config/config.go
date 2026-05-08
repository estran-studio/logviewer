// Package config handles configuration loading and validation.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// ErrContextNotFound is a sentinel error allowing callers to detect missing contexts via errors.Is.
var ErrContextNotFound = errors.New("context not found")

// Sentinel errors returned by LoadContextConfig so callers can detect exact
// failure modes using errors.Is().
var (
	ErrConfigParse = errors.New("invalid config content")
	ErrNoContexts  = errors.New("no contexts found in config file")
	ErrNoClients   = errors.New("no clients found in config file")
)

const (
	// EnvConfigPath is the environment variable used to override the config path
	EnvConfigPath = "LOGVIEWER_CONFIG"

	// DefaultConfigDir is the directory under the user's home where the config
	// file is expected when no explicit path or env var is provided.
	DefaultConfigDir = ".logviewer"

	// DefaultConfigFile is the config filename to look for in the default dir.
	DefaultConfigFile = "config.yaml"
)

// ResolveConfigPaths determines which configuration files to load based on precedence rules.
func ResolveConfigPaths(explicitPath string) ([]string, error) {
	var files []string

	if strings.TrimSpace(explicitPath) != "" {
		files = []string{explicitPath}
	} else if env := strings.TrimSpace(os.Getenv(EnvConfigPath)); env != "" {
		// Support "file1.yaml:file2.yaml"
		files = strings.Split(env, string(os.PathListSeparator))
	} else {
		// Default: Load ~/.logviewer/config.yaml AND ~/.logviewer/configs/*.yaml
		home, err := os.UserHomeDir()
		if err == nil {
			defaultDir := filepath.Join(home, DefaultConfigDir)

			// Main config
			main := filepath.Join(defaultDir, DefaultConfigFile)
			if _, err := os.Stat(main); err == nil {
				files = append(files, main)
			}

			// Drop-in directory for organization (e.g. personal.yaml, work.yaml)
			dropInDir := filepath.Join(defaultDir, "configs")
			if entries, err := os.ReadDir(dropInDir); err == nil {
				for _, e := range entries {
					if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
						files = append(files, filepath.Join(dropInDir, e.Name()))
					}
				}
			}
		}
	}

	if len(files) == 0 && explicitPath != "" {
		return nil, fmt.Errorf("config file not found at path: %s", explicitPath)
	}
	return files, nil
}

// LoadContextConfig loads configuration from one or multiple files and merges them.
// Prioritizes:
// 1. explicitPath if provided.
// 2. LOGVIEWER_CONFIG env var (can be colon-separated list).
// 3. Defaults: ~/.logviewer/config.yaml AND ~/.logviewer/configs/*.yaml.
func LoadContextConfig(explicitPath string) (*ContextConfig, error) {
	// 1. Determine list of files to load
	files, err := ResolveConfigPaths(explicitPath)
	if err != nil {
		return nil, err
	}

	// 2. Merge all configs
	mergedCfg := &ContextConfig{
		Clients:  make(Clients),
		Searches: make(Searches),
		Contexts: make(Contexts),
	}

	filesLoaded := 0
	for _, path := range files {
		// Check if file exists, if not and it was explicitly asked for or in env var, we might want to error.
		// For auto-discovery, we checked existence before adding.
		// However, for env var split, we might have non-existent files.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// If explicitly requested (via arg or env), error out.
			if explicitPath != "" || os.Getenv(EnvConfigPath) != "" {
				return nil, fmt.Errorf("config file not found at path: %s", path)
			}
			continue
		}

		partial, err := loadSingleFile(path)
		if err != nil {
			return nil, fmt.Errorf("error loading %s: %w", path, err)
		}

		// Merge Maps (Last file wins on collision)
		for k, v := range partial.Clients {
			mergedCfg.Clients[k] = v
		}
		for k, v := range partial.Searches {
			mergedCfg.Searches[k] = v
		}
		for k, v := range partial.Contexts {
			mergedCfg.Contexts[k] = v
		}
		filesLoaded++
	}

	if filesLoaded == 0 && (explicitPath != "" || os.Getenv(EnvConfigPath) != "") {
		// If user pointed to something and we loaded nothing, that's an error.
		// Re-using the error generation from original logic slightly.
		return nil, fmt.Errorf("config file not found")
	}

	// 3. Load Active State (Current Context)
	state, err := LoadState()
	if err != nil {
		// It's not a fatal error, but the user should know why their active context isn't working.
		fmt.Fprintf(os.Stderr, "Warning: could not load active context state: %v\n", err)
	} else {
		mergedCfg.CurrentContext = state.CurrentContext
	}

	if len(mergedCfg.Contexts) == 0 && filesLoaded > 0 {
		// If we loaded files but found no contexts, that's an error
		return nil, ErrNoContexts
	}

	// Ensure the clients map exists and provide a default "local" client
	if mergedCfg.Clients == nil {
		mergedCfg.Clients = Clients{}
	}
	if _, ok := mergedCfg.Clients["local"]; !ok {
		mergedCfg.Clients["local"] = Client{Type: "local", Options: ty.MI{}}
	}

	if err := validateClients(mergedCfg); err != nil {
		return nil, err
	}

	return mergedCfg, nil
}

func loadSingleFile(configPath string) (*ContextConfig, error) {
	// Read file contents and support JSON or YAML formats
	data, err := os.ReadFile(configPath) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var config ContextConfig
	ext := strings.ToLower(filepath.Ext(configPath))
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &config); err != nil {
			wrapped := fmt.Errorf("parsing JSON %s: %w", configPath, err)
			return nil, errors.Join(ErrConfigParse, wrapped)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &config); err != nil {
			wrapped := fmt.Errorf("parsing YAML %s: %w", configPath, err)
			return nil, errors.Join(ErrConfigParse, wrapped)
		}
	default:
		// Try JSON then YAML as a fallback
		if err := json.Unmarshal(data, &config); err == nil {
			break
		}
		if err := yaml.Unmarshal(data, &config); err == nil {
			break
		}
		return nil, fmt.Errorf("%w: unsupported or invalid config format for file: %s", ErrConfigParse, configPath)
	}
	return &config, nil
}

// validateClients performs lightweight validation of configured clients and
// returns a combined error describing any missing required options. This is
// intended to help users detect common config typos (e.g. using "option"
// instead of "options") and missing fields such as Url/Endpoint/Addr.
func validateClients(cc *ContextConfig) error {
	problems := []string{}

	for name, c := range cc.Clients {
		switch strings.ToLower(c.Type) {
		case "splunk":
			if c.Options.GetString("url") == "" {
				problems = append(problems, fmt.Sprintf("client '%s' (splunk) missing required option 'url'", name))
			}
		case "opensearch", "kibana":
			if c.Options.GetString("endpoint") == "" {
				problems = append(problems, fmt.Sprintf("client '%s' (%s) missing required option 'endpoint'", name, c.Type))
			}
		case "ssh":
			if c.Options.GetString("addr") == "" {
				problems = append(problems, fmt.Sprintf("client '%s' (ssh) missing required option 'addr'", name))
			}
		case "docker":
			// docker Host can be empty (falls back to unix socket), so just warn
			// but do not fail.
			// no-op
		default:
			// Unknown types are not validated here.
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid client configuration:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// Client represents a log source configuration.
type Client struct {
	Type    string `json:"type"`
	Options ty.MI  `json:"options"`
}

// PromptConfig holds optional customization for MCP prompt generation.
type PromptConfig struct {
	// Description overrides the auto-generated prompt description
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// ExampleQueries provides context-specific example queries for the prompt
	ExampleQueries []string `json:"exampleQueries,omitempty" yaml:"exampleQueries,omitempty"`
	// Disabled prevents prompt generation for this context
	Disabled bool `json:"disabled,omitempty" yaml:"disabled,omitempty"`
}

// SearchContext defines a searchable context with optional inheritance and prompt configuration.
type SearchContext struct {
	Description   string           `json:"description,omitempty" yaml:"description,omitempty"`
	Client        string           `json:"client" yaml:"client"`
	SearchInherit []string         `json:"searchInherit" yaml:"searchInherit"`
	Search        client.LogSearch `json:"search" yaml:"search"`
	Prompt        PromptConfig     `json:"prompt,omitempty" yaml:"prompt,omitempty"`
}

// Clients is a map of client configurations.
type Clients map[string]Client

// Searches is a map of named search definitions.
type Searches map[string]client.LogSearch

// Contexts is a map of search contexts.
type Contexts map[string]SearchContext

// ContextConfig is the top-level configuration structure.
type ContextConfig struct {
	Clients        `json:"clients" yaml:"clients"`
	Searches       `json:"searches" yaml:"searches"`
	Contexts       `json:"contexts" yaml:"contexts"`
	CurrentContext string `json:"-" yaml:"-"`
}

// GetSearchContext resolves a search context by ID, merging with defaults and overrides.
func (cc ContextConfig) GetSearchContext(contextID string, inherits []string, logSearch client.LogSearch, runtimeVars map[string]string) (SearchContext, error) {
	if contextID == "" {
		return SearchContext{}, errors.New("contextID is empty, required when using config")
	}

	searchContext, ok := cc.Contexts[contextID]
	if !ok {
		return SearchContext{}, fmt.Errorf("%w: %s", ErrContextNotFound, contextID)
	}

	// Deep copy the search to avoid mutating the original config's maps
	searchContext.Search = deepCopyLogSearch(searchContext.Search)

	// Combine inherits from context and the call
	allInherits := make([]string, 0, len(searchContext.SearchInherit)+len(inherits))
	allInherits = append(allInherits, searchContext.SearchInherit...)
	allInherits = append(allInherits, inherits...)
	if len(allInherits) > 0 {
		for _, inherit := range allInherits {
			inheritSearch, found := cc.Searches[inherit]
			if !found {
				return SearchContext{}, fmt.Errorf("failed to find a search context for %s", inherit)
			}
			if err := searchContext.Search.MergeInto(&inheritSearch); err != nil {
				return SearchContext{}, fmt.Errorf("failed to merge inherited search %s: %w", inherit, err)
			}
		}
	}

	// Merge the provided logSearch into the context's search
	if err := searchContext.Search.MergeInto(&logSearch); err != nil {
		return SearchContext{}, fmt.Errorf("failed to merge provided search: %w", err)
	}

	// Build complete variable map: defaults from variable definitions + runtime vars (runtime takes precedence)
	completeVars := make(map[string]string)
	// First, add defaults from variable definitions
	for varName, varDef := range searchContext.Search.Variables {
		if varDef.Default != nil {
			completeVars[varName] = fmt.Sprintf("%v", varDef.Default)
		}
	}
	// Then, override with runtime variables
	for k, v := range runtimeVars {
		completeVars[k] = v
	}

	// Resolve variables in all relevant fields
	searchContext.Search.Fields = searchContext.Search.Fields.ResolveVariablesWith(completeVars)
	searchContext.Search.FieldsCondition = searchContext.Search.FieldsCondition.ResolveVariablesWith(completeVars)
	searchContext.Search.Options = searchContext.Search.Options.ResolveVariablesWith(completeVars)

	// Resolve variables in Opt[string] fields
	if searchContext.Search.PrinterOptions.Template.Set {
		resolvedTemplate := ty.ResolveVars(searchContext.Search.PrinterOptions.Template.Value, completeVars)
		searchContext.Search.PrinterOptions.Template.S(resolvedTemplate)
	}
	if searchContext.Search.PrinterOptions.MessageRegex.Set {
		resolvedMessageRegex := ty.ResolveVars(searchContext.Search.PrinterOptions.MessageRegex.Value, completeVars)
		searchContext.Search.PrinterOptions.MessageRegex.S(resolvedMessageRegex)
	}
	if searchContext.Search.FieldExtraction.GroupRegex.Set {
		resolvedGroupRegex := ty.ResolveVars(searchContext.Search.FieldExtraction.GroupRegex.Value, completeVars)
		searchContext.Search.FieldExtraction.GroupRegex.S(resolvedGroupRegex)
	}
	if searchContext.Search.FieldExtraction.KvRegex.Set {
		resolvedKvRegex := ty.ResolveVars(searchContext.Search.FieldExtraction.KvRegex.Value, completeVars)
		searchContext.Search.FieldExtraction.KvRegex.S(resolvedKvRegex)
	}
	if searchContext.Search.FieldExtraction.TimestampRegex.Set {
		resolvedTimestampRegex := ty.ResolveVars(searchContext.Search.FieldExtraction.TimestampRegex.Value, completeVars)
		searchContext.Search.FieldExtraction.TimestampRegex.S(resolvedTimestampRegex)
	}

	return searchContext, nil
}

// deepCopyLogSearch creates a deep copy of a LogSearch to avoid mutating the original config.
// This is critical because maps are reference types - without deep copy, merging operations
// would permanently modify the original config's maps.
func deepCopyLogSearch(src client.LogSearch) client.LogSearch {
	dst := src // Copy all value types

	// Deep copy Fields (ty.MS = map[string]string)
	if src.Fields != nil {
		dst.Fields = make(ty.MS, len(src.Fields))
		for k, v := range src.Fields {
			dst.Fields[k] = v
		}
	}

	// Deep copy FieldsCondition (ty.MS = map[string]string)
	if src.FieldsCondition != nil {
		dst.FieldsCondition = make(ty.MS, len(src.FieldsCondition))
		for k, v := range src.FieldsCondition {
			dst.FieldsCondition[k] = v
		}
	}

	// Deep copy Options (ty.MI = map[string]interface{})
	if src.Options != nil {
		dst.Options = make(ty.MI, len(src.Options))
		for k, v := range src.Options {
			dst.Options[k] = v
		}
	}

	// Deep copy Variables (map[string]VariableDefinition)
	if src.Variables != nil {
		dst.Variables = make(map[string]client.VariableDefinition, len(src.Variables))
		for k, v := range src.Variables {
			dst.Variables[k] = v
		}
	}

	// Deep copy Filter (recursive structure)
	if src.Filter != nil {
		copied := deepCopyFilter(*src.Filter)
		dst.Filter = &copied
	}

	return dst
}

// deepCopyFilter creates a deep copy of a Filter (recursive AST).
func deepCopyFilter(src client.Filter) client.Filter {
	dst := src // Copy value fields

	// Deep copy nested Filters slice
	if src.Filters != nil {
		dst.Filters = make([]client.Filter, len(src.Filters))
		for i, f := range src.Filters {
			dst.Filters[i] = deepCopyFilter(f)
		}
	}

	return dst
}
