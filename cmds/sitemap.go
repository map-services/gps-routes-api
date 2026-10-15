//go:build !sqlite

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

func GenerateSitemap(host string) {

	godx.GitVersion()
	godx.EnvironmentVars()
	godx.UserInfo()

	dbConfig := db.ConfigFromEnv()
	ctx := context.Background()
	pool, err := db.NewDBPool(ctx, dbConfig)
	if err != nil {
		log.Fatalf("Failed to create connection pool: %v", err)
	}
	defer pool.Close()

	pg := repositories.NewPostgresRouteRepository(pool, dbConfig.Schema)
	routes, err := pg.SearchHits(ctx, &request.SearchRequest{Limit: 1_000_000})
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
		url := stm.URL{
			{"loc", url.QueryEscape(route.Ref)},
			{"lastmod", now},
		}

		if route.HeadlineImageUrl != nil {
			url = append(url, stm.URL{
				{"image", stm.URL{
					{"loc", *route.HeadlineImageUrl},
				}},
			}...)
		}
		sm.Add(url)
		if err := bar.Add(1); err != nil {
			log.Fatalf("issue with progress bar: %v", err)
		}
	}

	// Write sitemap to file
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
