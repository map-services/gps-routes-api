package cmds

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/map-services/gps-routes-api/db"
	"github.com/rm-hull/godx"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/stdlib"
)

type loggerBridge struct {
	verbose bool
	logger  *log.Logger
}

func (l *loggerBridge) Printf(format string, v ...any) {
	l.logger.Printf(format, v...)
}

func (l *loggerBridge) Verbose() bool {
	return l.verbose
}

func RunMigration(direction string, migrationsPath string) {

	godx.GitVersion()
	godx.EnvironmentVars()
	godx.UserInfo()

	config := db.ConfigFromEnv()
	ctx := context.Background()
	pool, err := db.NewDBPool(ctx, config)
	if err != nil {
		log.Fatalf("Failed to create connection pool: %v", err)
	}
	defer pool.Close()

	db := stdlib.OpenDB(*pool.Config().ConnConfig)
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("failed to close database connection: %v", err)
		}
	}()

	// Create the migrate instance
	driver, err := postgres.WithInstance(db, &postgres.Config{
		DatabaseName: config.DBName,
		SchemaName:   config.Schema,
	})
	if err != nil {
		log.Fatalf("error creating postgres driver: %v", err)
	}

	_, err = db.Exec(fmt.Sprintf("SET search_path TO %s, public", config.Schema))
	if err != nil {
		log.Fatalf("failed to set search path: %v", err)
	}

	absPath, err := filepath.Abs(migrationsPath)
	if err != nil {
		log.Fatalf("failed convert to convert migrations path to absolute path: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+absPath,
		config.DBName,
		driver,
	)
	if err != nil {
		log.Fatalf("failed to create migrate instance: %v", err)
	}

	m.Log = &loggerBridge{
		verbose: true,
		logger:  log.New(os.Stdout, "", log.LstdFlags),
	}

	m.Log.Printf("Attempting to migrate %s from directory: %s", direction, migrationsPath)

	// Run migrations
	if direction == "up" {
		err = m.Up()
	} else {
		err = m.Down()
	}
	if err != nil {
		if err == migrate.ErrNoChange {
			m.Log.Printf("No changes applied")
		} else {
			log.Fatalf("migrations failed: %v", err)
		}
		return
	}
	m.Log.Printf("Migration applied")
}
