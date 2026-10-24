package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/map-services/gps-routes-api/db"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/repositories"
	"github.com/rm-hull/godx"
	"github.com/schollz/progressbar/v3"
)

// isRunningInDocker reports whether the process is running inside a container.
func isRunningInDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return !os.IsNotExist(err)
}

// walkFiles recursively walks through a folder and returns the relative paths
// for files.
func walkFiles(root string, maxFiles int) ([]string, error) {
	var files []string

	// Walk through the root directory and subdirectories.
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only add files, not directories.
		if !info.IsDir() && len(files) < maxFiles {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}

// loadJson reads and unmarshals a JSON file into a RouteMetadata struct.
func loadJson(filename string) (*domain.RouteMetadata, error) {
	var metadata domain.RouteMetadata

	// Read the file content.
	fileContent, err := os.ReadFile(filename)
	if err != nil {
		return &metadata, fmt.Errorf("could not read file: %v", err)
	}

	// Unmarshal JSON content into RouteMetadata struct.
	err = json.Unmarshal(fileContent, &metadata)
	if err != nil {
		return &metadata, fmt.Errorf("could not unmarshal JSON: %v", err)
	}

	return &metadata, nil
}

// ImportData loads route metadata from JSON backup files at path into a SQLite
// database (path from SQLITE_DB_PATH env, default data/routes.db).
//
// The SQLite schema is (re)created from db/migrations/00002_sqlite_schema.sql
// (via setupSQLiteDB) before import. The schema includes an R*Tree virtual table
// (routes_rtree) with triggers that auto-populate bbox entries on INSERT, so
// every imported route is automatically indexable for bbox + haversine queries
// — no Spatialite extension required.
//
// When overwrite is true the database file at config.Path is deleted before
// schema creation, ensuring a clean import with no leftover rows.
func ImportData(path string, maxRecords int, overwrite bool) {
	godx.Diagnostics(slog.Default())

	config := db.SQLiteConfigFromEnv()
	config.ReadOnly = false // Import needs write access to store routes and run VACUUM.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if overwrite {
		if err := os.Remove(config.Path); err != nil && !os.IsNotExist(err) {
			slog.Error("failed to remove existing database file", "path", config.Path, "error", err)
			os.Exit(1)
		}
	}

	if err := setupSQLiteDB(config.Path, false); err != nil {
		slog.Error("failed to initialise SQLite schema", "error", err)
		os.Exit(1)
	}

	sqlDB, err := db.NewSQLiteDB(ctx, config)
	if err != nil {
		slog.Error("failed to open SQLite database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = sqlDB.Close() }()

	repo := repositories.NewSQLiteRouteRepository(sqlDB)

	files, err := walkFiles(path, maxRecords)
	if err != nil {
		slog.Error("failed to import data", "error", err)
		os.Exit(1)
	}

	totalRecords := int64(len(files))
	isDocker := isRunningInDocker()
	var bar *progressbar.ProgressBar
	if isDocker {
		slog.Info("Detected likely running inside docker container")
		bar = progressbar.DefaultSilent(totalRecords)
	} else {
		bar = progressbar.Default(totalRecords)
	}

	// Phase 1: Load all JSON files into memory.
	routes := make([]*domain.RouteMetadata, 0, len(files))
	for idx, file := range files {
		if err := bar.Add(1); err != nil {
			slog.Error("issue with progress bar", "error", err)
			os.Exit(1)
		}
		data, err := loadJson(file)
		if err != nil {
			slog.Error("failed to load file", "file", file, "error", err)
			os.Exit(1)
		}
		routes = append(routes, data)

		if isDocker && idx%37 == 0 {
			slog.Info("Loaded records", "count", idx)
		}
	}

	// Phase 2: Insert all routes in a single transaction with prepared statements.
	// This is orders of magnitude faster than per-route transactions because it
	// avoids per-commit fsync overhead and batches FTS/R*Tree trigger execution.
	slog.Info("Inserting routes into SQLite", "count", len(routes))
	if err := repo.StoreBatch(ctx, routes); err != nil {
		slog.Error("failed to batch insert routes", "error", err)
		os.Exit(1)
	}

	// Run post-import database optimization (ANALYZE, VACUUM, PRAGMA optimize)
	// to update query planner statistics and compact the database file.
	slog.Info("Starting post-import database optimization")
	if err := db.OptimizeDatabase(sqlDB); err != nil {
		slog.Error("failed to optimize database after import", "error", err)
		os.Exit(1)
	}

	if isDocker {
		slog.Info("Import completed", "records", totalRecords, "path", config.Path)
	} else {
		slog.Info("Imported records into SQLite", "records", totalRecords, "path", config.Path)
	}
}
