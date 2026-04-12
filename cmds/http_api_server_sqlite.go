//go:build sqlite

package cmds

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Depado/ginprom"
	ratelimit "github.com/JGLTechnologies/gin-rate-limit"
	"github.com/aurowora/compress"
	"github.com/gin-contrib/cors"
	limits "github.com/gin-contrib/size"
	"github.com/gin-gonic/gin"
	healthcheck "github.com/tavsec/gin-healthcheck"
	"github.com/tavsec/gin-healthcheck/checks"
	hc_config "github.com/tavsec/gin-healthcheck/config"
	cachecontrol "go.eigsys.de/gin-cachecontrol/v2"

	"github.com/rm-hull/godx"
	"github.com/rm-hull/gps-routes-api/db"
	"github.com/rm-hull/gps-routes-api/middlewares"
	"github.com/rm-hull/gps-routes-api/repositories"
	"github.com/rm-hull/gps-routes-api/routes"
	"github.com/rm-hull/gps-routes-api/services"
	"github.com/rm-hull/gps-routes-api/services/osdatahub"
)

func NewHttpApiServer(port int) {

	godx.GitVersion()
	godx.EnvironmentVars()
	godx.UserInfo()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	engine := gin.New()

	prometheus := ginprom.New(
		ginprom.Engine(engine),
		ginprom.Path("/metrics"),
		ginprom.Ignore("/healthz"),
	)

	rlStore := ratelimit.InMemoryStore(&ratelimit.InMemoryOptions{
		Rate:  time.Second,
		Limit: 5,
	})
	rateLimiter := ratelimit.RateLimiter(rlStore, &ratelimit.Options{
		ErrorHandler: func(c *gin.Context, info ratelimit.Info) {
			c.JSON(429, gin.H{
				"error":   "Too many requests",
				"message": "Try again in " + time.Until(info.ResetTime).String(),
			})
		},
		KeyFunc: func(c *gin.Context) string {
			return c.ClientIP()
		},
	})

	engine.Use(
		gin.LoggerWithWriter(gin.DefaultWriter, "/healthz", "/metrics"),
		gin.Recovery(),
		cors.New(cors.Config{
			AllowAllOrigins: true,
			AllowHeaders:    []string{"X-API-Key", "Origin", "Content-Length", "Content-Type"},
		}),
		middlewares.AuthMiddleware("/healthz"),
		middlewares.ErrorHandler(),
		compress.Compress(),
		limits.RequestSizeLimiter(10*1024),
		cachecontrol.New(cachecontrol.CacheAssetsForeverPreset),
		prometheus.Instrument(),
		rateLimiter,
	)

	// SQLite setup
	sqliteConfig := db.SQLiteConfigFromEnv()
	sqliteDB, err := db.NewSQLiteDB(ctx, sqliteConfig)
	if err != nil {
		log.Fatalf("failed to create SQLite database: %v", err)
	}
	defer func() {
		if err := sqliteDB.Close(); err != nil {
			log.Printf("failed to close SQLite database: %v", err)
		}
	}()

	err = healthcheck.New(engine, hc_config.DefaultConfig(), []checks.Check{
		checks.SqlCheck{Sql: sqliteDB},
	})
	if err != nil {
		log.Fatalf("failed to initialize healthcheck: %v", err)
	}

	namesApi := osdatahub.NewNamesApi(prometheus, "https://api.os.uk/search/names/v1", os.Getenv("OS_NAMES_API_KEY"))
	// repo := repositories.NewCachedRepository(prometheus, repositories.NewSQLiteRouteRepository(sqliteDB))
	repo := repositories.NewSQLiteRouteRepository(sqliteDB)
	service := services.NewRoutesService(repo, namesApi)

	router := routes.NewRouterWithGinEngine(engine, routes.ApiHandleFunctions{
		RoutesAPI: routes.RoutesAPI{
			Service: service,
		},
	})

	log.Printf("Starting HTTP API Server on port %d...", port)
	err = router.Run(fmt.Sprintf(":%d", port))
	log.Fatalf("HTTP API Server failed to start on port %d: %v", port, err)
}
