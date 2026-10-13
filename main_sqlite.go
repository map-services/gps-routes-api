//go:build sqlite

package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/map-services/gps-routes-api/cmds"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	var rootCmd = &cobra.Command{
		Use:  "gps-routes",
		Long: `HTTP server, DB migration and data import/export`,
	}

	var apiServerCmd = &cobra.Command{
		Use:   "api-server [port]",
		Short: "Start HTTP API server",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			port, err := strconv.Atoi(args[0])
			if err != nil {
				log.Fatalf("Invalid port number: %v", err)
			}
			cmds.NewHttpApiServer(port)
		},
	}

	var migrateDataCmd = &cobra.Command{
		Use:   "migrate-data",
		Short: "Migrate data from PostgreSQL to SQLite",
		Long:  "Migrate routes data from PostgreSQL to SQLite with transformation (arrays→JSON, geometry→WKB, TSVECTOR→FTS5).\nUses POSTGRES_* environment variables if --pg-url not specified.\n⚠️  Note: Falls back to basic schema if FTS5 not available (no full-text search).",
		Run: func(cmd *cobra.Command, args []string) {
			pgURL, _ := cmd.Flags().GetString("pg-url")
			sqliteDB, _ := cmd.Flags().GetString("sqlite-db")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			maxRecords, _ := cmd.Flags().GetInt("max-records")
			cmds.MigratePostgresToSQLite(pgURL, sqliteDB, dryRun, maxRecords)
		},
	}
	migrateDataCmd.Flags().String("pg-url", "", "PostgreSQL connection URL (default: from env)")
	migrateDataCmd.Flags().String("sqlite-db", "./data/routes.db", "SQLite database file path")
	migrateDataCmd.Flags().Bool("dry-run", false, "Dry run - don't actually migrate data")
	migrateDataCmd.Flags().Int("max-records", 0, "Maximum records to migrate (0 = all)")

	var importCmd = &cobra.Command{
		Use:   "import [path]",
		Short: "Import JSON route data from specified path into SQLite",
		Long:  "Imports route metadata from JSON backup files at path into the SQLite database at SQLITE_DB_PATH (default data/routes.db).\nThe schema is (re)created from db/migrations/00002_sqlite_schema.sql before import. Import only writes rows; Spatialite-dependent bbox search requires Spatialite at query time.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cmds.ImportData(args[0], math.MaxInt64)
		},
	}

	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(apiServerCmd)
	rootCmd.AddCommand(migrateDataCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
