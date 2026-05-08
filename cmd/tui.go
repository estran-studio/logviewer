// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"fmt"
	"os"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/factory"
	"github.com/estran-studio/logviewer/pkg/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:     "tui",
	Aliases: []string{"live", "ui"},
	Short:   "Launch interactive TUI (Alpha) for log viewing",
	Long: `Launch an interactive Terminal User Interface (Alpha) for browsing and filtering logs.

The TUI provides:
  - Tab-based navigation between multiple contexts
  - Real-time log streaming
  - Vim-style navigation (j/k, gg, G)
  - Full-text search with /
  - Detailed JSON field inspection in sidebar

> **Note:** The TUI is currently in **Alpha**. Features may change.

Examples:
  # Launch TUI with current context
  logviewer tui

  # Launch TUI with specific context(s)
  logviewer tui -i prod-logs
  logviewer tui -i prod-logs -i staging-logs

  # Launch TUI with filters
  logviewer tui -i prod-logs -f level=ERROR --last 1h

  # Launch TUI with query
  logviewer tui -i prod-logs -q "level=ERROR AND service=api"`,
	PreRun: onCommandStart,
	Run:    runTUI,
}

func runTUI(_ *cobra.Command, _ []string) {
	// Load configuration
	cfg, _, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintln(os.Stderr, "Tip: Run 'logviewer configure' to set up a configuration.")
		os.Exit(1)
	}

	// Create factories
	clientFactory, err := factory.GetLogBackendFactory(cfg.Clients)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating client factory: %v\n", err)
		os.Exit(1)
	}

	searchFactory, err := factory.GetLogSearchFactory(clientFactory, *cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating search factory: %v\n", err)
		os.Exit(1)
	}

	// Build search request from flags
	searchRequest := buildSearchRequest()

	// Get runtime variables
	runtimeVars := parseRuntimeVars()

	// Resolve context IDs
	resolvedContextIDs := resolveContextIDsFromConfig(cfg)
	if len(resolvedContextIDs) == 0 {
		// If no context specified, try to show available contexts
		if len(cfg.Contexts) > 0 {
			fmt.Fprintln(os.Stderr, "No context specified. Available contexts:")
			for id := range cfg.Contexts {
				fmt.Fprintf(os.Stderr, "  - %s\n", id)
			}
			fmt.Fprintln(os.Stderr, "\nUse: logviewer tui -i <context-id>")
			fmt.Fprintln(os.Stderr, "Or set a default: logviewer context use <context-id>")
		} else {
			fmt.Fprintln(os.Stderr, "No contexts defined in configuration.")
			fmt.Fprintln(os.Stderr, "Run 'logviewer configure' to set up contexts.")
		}
		os.Exit(1)
	}

	// Create TUI model
	model := tui.New(cfg, clientFactory, searchFactory)
	model.RuntimeVars = runtimeVars
	model.InitialContexts = resolvedContextIDs
	model.InitialInherits = inherits
	searchCopy := deepCopyLogSearch(searchRequest)
	model.InitialSearch = &searchCopy

	// Create the bubbletea program
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Run the TUI
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}

	// Check if there was an error
	if m, ok := finalModel.(tui.Model); ok {
		for _, tab := range m.Tabs {
			if tab.Error != nil {
				fmt.Fprintf(os.Stderr, "Tab '%s' had error: %v\n", tab.Name, tab.Error)
			}
		}
	}
}

// deepCopyLogSearch creates a deep copy of a LogSearch to avoid shared references.
// Based on config.deepCopyLogSearch but adapted to avoid circular dependencies.
func deepCopyLogSearch(src client.LogSearch) client.LogSearch {
	dst := src // Copy all value types

	// Deep copy Fields
	if src.Fields != nil {
		dst.Fields = make(map[string]string, len(src.Fields))
		for k, v := range src.Fields {
			dst.Fields[k] = v
		}
	}

	// Deep copy FieldsCondition
	if src.FieldsCondition != nil {
		dst.FieldsCondition = make(map[string]string, len(src.FieldsCondition))
		for k, v := range src.FieldsCondition {
			dst.FieldsCondition[k] = v
		}
	}

	// Deep copy Options
	if src.Options != nil {
		dst.Options = make(map[string]interface{}, len(src.Options))
		for k, v := range src.Options {
			dst.Options[k] = v
		}
	}

	// Deep copy Variables
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

// deepCopyFilter recursively copies a Filter AST to avoid shared slice references
func deepCopyFilter(src client.Filter) client.Filter {
	dst := src // Copy value fields

	// Deep copy nested Filters slice (recursive)
	if src.Filters != nil {
		dst.Filters = make([]client.Filter, len(src.Filters))
		for i, f := range src.Filters {
			dst.Filters[i] = deepCopyFilter(f) // Recursive
		}
	}

	return dst
}
