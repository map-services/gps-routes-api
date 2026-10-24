package main

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/map-services/gps-routes-api/cmds"
	"github.com/map-services/gps-routes-api/internal/logging"
)

func main() {
	var logLevelVar slog.LevelVar

	handler := slog.NewJSONHandler(
		os.Stderr,
		&slog.HandlerOptions{
			Level:       &logLevelVar,
			AddSource:   true,
			ReplaceAttr: logging.ReplaceAttr,
		},
	)
	logger := slog.New(handler)
	slog.SetDefault(logger)
	logging.BridgeStandardLog(handler)

	if err := godotenv.Load(); err != nil {
		logger.Warn("No .env file found")
	}

	// Watch for LOG_LEVEL env var and adjust dynamically.
	logLevelVar.Set(logging.ParseLogLevel(os.Getenv("LOG_LEVEL")))

	var rootCmd = &cobra.Command{
		Use:  "gps-routes",
		Long: `HTTP server, DB migration and data import/export`,
	}

	var importCmd = &cobra.Command{
		Use:   "import [path]",
		Short: "Import JSON route data from specified path into SQLite",
		Long:  "Imports route metadata from JSON backup files at path into the SQLite database at SQLITE_DB_PATH (default data/routes.db).\nThe schema is (re)created from db/migrations/00002_sqlite_schema.sql before import. Import only writes rows; bbox + haversine queries work via the built-in R*Tree + haversine_distance function (no Spatialite required).",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			overwrite, _ := cmd.Flags().GetBool("overwrite")
			cmds.ImportData(args[0], math.MaxInt64, overwrite)
		},
	}

	var apiServerCmd = &cobra.Command{
		Use:   "api-server [port]",
		Short: "Start HTTP API server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid port number: %w", err)
			}
			cmds.NewHttpApiServer(port)
			return nil
		},
	}

	var sitemapCmd = &cobra.Command{
		Use:   "sitemap [base_host_url]",
		Short: "Generate sitemap.xml",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cmds.GenerateSitemap(args[0])
		},
	}

	var versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(versioninfo.Short())
		},
	}

	rootCmd.AddCommand(importCmd)
	importCmd.Flags().Bool("overwrite", false, "Delete the SQLite database file before importing, starting fresh")
	rootCmd.AddCommand(apiServerCmd)
	rootCmd.AddCommand(sitemapCmd)
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		logger.Error("Failed to execute command", "error", err)
		os.Exit(1)
	}
}
