//go:build sqlite

package cmds

import (
	"context"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/ikeikeikeike/go-sitemap-generator/v2/stm"
	"github.com/map-services/gps-routes-api/db"
	"github.com/map-services/gps-routes-api/models/request"
	"github.com/map-services/gps-routes-api/repositories"
	"github.com/rm-hull/godx"
	"github.com/schollz/progressbar/v3"
)

// GenerateSitemap writes sitemap.xml from the SQLite database. This is the
// sqlite-tagged counterpart to the PostgreSQL implementation in sitemap.go
// (//go:build !sqlite); both are selected by build tag so that main.go (!sqlite,
// package build) and main_sqlite.go (sqlite, package build) each resolve
// `cmds.GenerateSitemap`.
//
// The sqlite main package (main_sqlite.go) does not register a sitemap cobra
// command, but this symbol IS required so a single-file debug build of main.go
// (e.g. `go build -tags=sqlite,sqlite_fts5 ./main.go`, which the debugger uses)
// resolves — normally main.go carries `//go:build !sqlite` and is omitted from
// the shipped sqlite binary, so GenerateSitemap is only exercised there / when
// run via that single-file debug build.
func GenerateSitemap(host string) {
	godx.GitVersion()
	godx.EnvironmentVars()
	godx.UserInfo()

	config := db.SQLiteConfigFromEnv()
	ctx := context.Background()

	sqlDB, err := db.NewSQLiteDB(ctx, config)
	if err != nil {
		log.Fatalf("failed to open SQLite database: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	repo := repositories.NewSQLiteRouteRepository(sqlDB)

	routes, err := repo.SearchHits(ctx, &request.SearchRequest{Limit: 1_000_000})
	if err != nil {
		log.Fatalf("Failed to get routes: %v", err)
	}

	sm := stm.NewSitemap(1)
	sm.SetDefaultHost(host)
	sm.SetPretty(true)
	sm.SetCompress(true)
	sm.Create()

	now := time.Now().Format("2006-01-02")
	bar := progressbar.Default(int64(len(*routes)))
	for _, route := range *routes {
		entry := stm.URL{
			{"loc", url.QueryEscape(route.Ref)},
			{"lastmod", now},
		}

		if route.HeadlineImageUrl != nil {
			entry = append(entry, stm.URL{
				{"image", stm.URL{
					{"loc", *route.HeadlineImageUrl},
				}},
			}...)
		}

		sm.Add(entry)
		if err := bar.Add(1); err != nil {
			log.Fatalf("issue with progress bar: %v", err)
		}
	}

	// Write sitemap to file (relative to the process CWD, matching sitemap.go).
	f, err := os.Create("sitemap.xml")
	if err != nil {
		log.Fatalf("failed to create sitemap file: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("failed to close sitemap file: %v", err)
		}
	}()

	if _, err := f.Write(sm.XMLContent()); err != nil {
		log.Fatalf("failed to write sitemap: %v", err)
	}

	log.Printf("Sitemap generated")
}
