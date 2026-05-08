package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Manage configuration contexts",
}

var useContextCmd = &cobra.Command{
	Use:   "use [context-id]",
	Short: "Set the current active context",
	// Autocomplete for context IDs
	ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		// We use configPath if set, otherwise default loading mechanism
		cfg, _ := config.LoadContextConfig(configPath)
		var suggestions []string
		if cfg != nil {
			for id := range cfg.Contexts {
				suggestions = append(suggestions, id)
			}
		}
		return suggestions, cobra.ShellCompDirectiveNoFileComp
	},
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			_ = cmd.Help()
			return
		}
		contextID := args[0]

		cfg, err := config.LoadContextConfig(configPath)
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}

		if _, ok := cfg.Contexts[contextID]; !ok {
			fmt.Printf("Error: context '%s' not found in any loaded config.\n", contextID)
			os.Exit(1)
		}

		state := &config.State{CurrentContext: contextID}
		if err := config.SaveState(state); err != nil {
			fmt.Printf("Error saving state: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Switched to context \"%s\".\n", contextID)
	},
}

var listContextsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available contexts",
	Run: func(_ *cobra.Command, _ []string) {
		cfg, err := config.LoadContextConfig(configPath)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		_, _ = fmt.Fprintln(w, "CURRENT\tNAME\tCLIENT\tDESCRIPTION")

		// Sort keys for consistent output
		keys := make([]string, 0, len(cfg.Contexts))
		for k := range cfg.Contexts {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, name := range keys {
			ctx := cfg.Contexts[name]
			prefix := " "
			if name == cfg.CurrentContext {
				prefix = "*"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", prefix, name, ctx.Client, ctx.Description)
		}
		_ = w.Flush()
	},
}

func init() {
	contextCmd.AddCommand(useContextCmd)
	contextCmd.AddCommand(listContextsCmd)
	rootCmd.AddCommand(contextCmd)
}
