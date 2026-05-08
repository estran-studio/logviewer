package cmd

import (
	"errors"
	"fmt"

	"github.com/estran-studio/logviewer/pkg/log/client/config"
)

func loadConfig(path string) (*config.ContextConfig, []string, error) {
	var cfg *config.ContextConfig
	var err error
	var files []string

	// Resolve paths first to know what we are loading
	files, err = config.ResolveConfigPaths(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve config paths: %w", err)
	}

	// Load using the resolved paths (or let LoadContextConfig re-resolve, but better to be consistent)
	// Since LoadContextConfig takes a single explicit path or relies on env/defaults,
	// and we want to return the files used, we can just call LoadContextConfig(path)
	// as it now uses ResolveConfigPaths internally.
	// However, to be precise, we should probably just use the files we resolved if we could,
	// but LoadContextConfig doesn't take a list of files.
	// So we will trust that LoadContextConfig(path) does the same thing as ResolveConfigPaths(path).

	cfg, err = config.LoadContextConfig(path)

	if err != nil {
		errorMsg := "failed to load context config"
		switch {
		case errors.Is(err, config.ErrConfigParse):
			errorMsg = "invalid configuration file format"
		case errors.Is(err, config.ErrNoClients):
			errorMsg = "configuration missing 'clients' section"
		case errors.Is(err, config.ErrNoContexts):
			errorMsg = "configuration missing 'contexts' section"
		}
		if path != "" {
			return nil, nil, fmt.Errorf("%s %s: %w", errorMsg, path, err)
		}
		return nil, nil, fmt.Errorf("%s: %w", errorMsg, err)
	}
	return cfg, files, nil
}
