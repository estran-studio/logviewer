package cmd

import (
	"errors"
	"log/slog"
	"os"
	"strconv"

	"github.com/estran-studio/logviewer/pkg/api"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/server"
	"github.com/spf13/cobra"
)

var (
	port int
	host string
)

var serverCmd = &cobra.Command{
	Use:    "server",
	Short:  "Start the logviewer server",
	Long:   `Starts an HTTP server to query logs, providing a programmatic API.`,
	PreRun: onCommandStart,
	Run: func(_ *cobra.Command, _ []string) {
		// NOTE: This implementation assumes a logger is configured and available via `onCommandStart`.
		// A basic logger is created here as an example. You should integrate this with your application's logging strategy.
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

		logger.Info("loading configuration", "path", configPath)
		cfg, err := config.LoadContextConfig(configPath)
		if err != nil {
			// Provide a clearer, actionable message depending on the error type.
			switch {
			case errors.Is(err, config.ErrConfigParse):
				logger.Error("invalid configuration file format", "path", configPath, "err", err, "hint", "check YAML/JSON syntax and types")
			case errors.Is(err, config.ErrNoClients):
				logger.Error("configuration missing 'clients' section", "path", configPath, "err", err, "hint", "add a 'clients' section mapping client IDs to client configs")
			case errors.Is(err, config.ErrNoContexts):
				logger.Error("configuration missing 'contexts' section", "path", configPath, "err", err, "hint", "add a 'contexts' section describing searchable contexts")
			default:
				logger.Error("failed to load configuration", "path", configPath, "err", err)
			}
			os.Exit(1)
		}

		s, err := server.NewServer(host, strconv.Itoa(port), cfg, logger, api.OpenAPISpec)
		if err != nil {
			logger.Error("failed to create server", "err", err)
			os.Exit(1)
		}

		if err := s.Start(); err != nil {
			logger.Error("server failed to start", "err", err)
			os.Exit(1)
		}
	},
}

func init() {
	serverCmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to listen on")
	serverCmd.Flags().StringVarP(&host, "host", "H", "0.0.0.0", "Host to bind to")
}
