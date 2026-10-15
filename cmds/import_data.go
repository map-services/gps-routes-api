package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
// (via setupSQLiteDB) before import. Import only INSERTs routes/nearby/images/details
// and does not require Spatialite; the spatial bbox predicates (SQLiteDialect
// -> BuildMbr) require Spatialite at query time.
func ImportData(path string, maxRecords int) {
	godx.GitVersion()
	godx.EnvironmentVars()
	godx.UserInfo()

	config := db.SQLiteConfigFromEnv()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if err := setupSQLiteDB(config.Path, false); err != nil {
		log.Fatalf("failed to initialise SQLite schema: %v", err)
	}

	sqlDB, err := db.NewSQLiteDB(ctx, config)
	if err != nil {
		log.Fatalf("failed to open SQLite database: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	repo := repositories.NewSQLiteRouteRepository(sqlDB)

	files, err := walkFiles(path, maxRecords)
	if err != nil {
		log.Fatalf("failed to import data: %v", err)
	}

	totalRecords := int64(len(files))
	isDocker := isRunningInDocker()
	var bar *progressbar.ProgressBar
	if isDocker {
		log.Println("Detected likely running inside docker container")
		bar = progressbar.DefaultSilent(totalRecords)
	} else {
		bar = progressbar.Default(totalRecords)
	}

	for idx, file := range files {
		if err := bar.Add(1); err != nil {
			log.Fatalf("issue with progress bar: %v", err)
		}
		data, err := loadJson(file)
		if err != nil {
			log.Fatalf("failed to load file: %s: %v", file, err)
		}

		if err := repo.Store(ctx, data); err != nil {
			log.Fatalf("failed to store objectID: %s: %v", data.ObjectID, err)
		}

		if isDocker && idx%37 == 0 {
			log.Printf("Processed %d records...\n", idx)
		}
	}

	if isDocker {
		log.Printf("Completed: imported %d records into SQLite at %s", totalRecords, config.Path)
	} else {
		log.Printf("Imported %d records into SQLite at %s", totalRecords, config.Path)
	}
}
