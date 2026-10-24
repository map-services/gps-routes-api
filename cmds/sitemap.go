package cmds

import (
	"context"
	"log/slog"
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

// GenerateSitemap writes sitemap.xml from the SQLite database.
func GenerateSitemap(host string) {
	godx.Diagnostics(slog.Default())

	config := db.SQLiteConfigFromEnv()
	ctx := context.Background()

	sqlDB, err := db.NewSQLiteDB(ctx, config)
	if err != nil {
		slog.Error("failed to open SQLite database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = sqlDB.Close() }()

	repo := repositories.NewSQLiteRouteRepository(sqlDB)

	routes, err := repo.SearchHits(ctx, &request.SearchRequest{Limit: 1_000_000})
	if err != nil {
		slog.Error("Failed to get routes", "error", err)
		os.Exit(1)
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
			slog.Error("issue with progress bar", "error", err)
			os.Exit(1)
		}
	}

	// Write sitemap to file (relative to the process CWD, matching sitemap.go).
	f, err := os.Create("sitemap.xml")
	if err != nil {
		slog.Error("failed to create sitemap file", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Warn("failed to close sitemap file", "error", err)
		}
	}()

	if _, err := f.Write(sm.XMLContent()); err != nil {
		slog.Error("failed to write sitemap", "error", err)
		os.Exit(1)
	}

	slog.Info("Sitemap generated")
}
