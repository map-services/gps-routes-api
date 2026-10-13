//go:build !sqlite

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

// ImportData walks JSON backup files under path and stores them through the
// PostgreSQL repository. The SQLite counterpart lives in import_data_sqlite.go;
// both are selected by build tag so the CLI entry points can both call
// cmds.ImportData(path, maxRecords).
func ImportData(path string, maxRecords int) {
	godx.GitVersion()
	godx.EnvironmentVars()
	godx.UserInfo()

	config := db.ConfigFromEnv()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	pool, err := db.NewDBPool(ctx, config)
	if err != nil {
		log.Fatalf("failed to create database pool: %v", err)
	}
	defer pool.Close()

	repo := repositories.NewPostgresRouteRepository(pool, config.Schema)

	files, err := walkFiles(path, maxRecords)
	if err != nil {
		log.Fatalf("failed to import data: %v", err)
	}

	isDocker := isRunningInDocker()
	totalRecords := int64(len(files))
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

		err = repo.Store(ctx, data)
		if err != nil {
			log.Fatalf("failed to store objectID: %s: %v", data.ObjectID, err)
		}

		if isDocker && idx%37 == 0 {
			log.Printf("Processed %d records...\n", idx)
		}
	}

	if isDocker {
		log.Printf("Completed: imported %d records\n", totalRecords)
	}
}
