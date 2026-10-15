package cmds

import (
	"context"
	"log"
	"time"

	"github.com/map-services/gps-routes-api/db"
	"github.com/map-services/gps-routes-api/repositories"
	"github.com/rm-hull/godx"
	"github.com/schollz/progressbar/v3"
)

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
