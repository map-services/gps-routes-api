package metrics

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const metricsNamespace = "gps_routes"

// DBStatsCollector periodically collects sql.DB.Stats() and exposes them
// as Prometheus gauges. It is database-agnostic and works with
// any driver that implements *sql.DB, including SQLite.
type DBStatsCollector struct {
	db     *sql.DB
	ticker *time.Ticker
	stop   chan struct{}

	// Prometheus gauges
	gaugeConnectionsMax           prometheus.Gauge
	gaugeConnectionsOpen          prometheus.Gauge
	gaugeConnectionsInUse         prometheus.Gauge
	gaugeConnectionsIdle          prometheus.Gauge
	gaugeConnectionsWaitCount     prometheus.Gauge
	gaugeConnectionsWaitDuration  prometheus.Gauge
	gaugeConnectionsMaxIdleClosed prometheus.Gauge
	gaugeConnectionsMaxIdleTime   prometheus.Gauge
	gaugeConnectionsMaxLifetime   prometheus.Gauge
}

// NewDBStatsCollector creates a new collector that registers gauges for
// database connection pool statistics and starts a background goroutine to
// update them at the given interval. Call Stop() when the server shuts down.
func NewDBStatsCollector(db *sql.DB, interval time.Duration) *DBStatsCollector {
	collector := &DBStatsCollector{
		db:     db,
		ticker: time.NewTicker(interval),
		stop:   make(chan struct{}),

		gaugeConnectionsMax: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "db_connections_max",
			Help:      "Maximum number of open connections to the database",
		}),
		gaugeConnectionsOpen: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "db_connections_open",
			Help:      "Number of open connections to the database",
		}),
		gaugeConnectionsInUse: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "db_connections_in_use",
			Help:      "Number of connections currently in use",
		}),
		gaugeConnectionsIdle: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "db_connections_idle",
			Help:      "Number of idle connections to the database",
		}),
		gaugeConnectionsWaitCount: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "db_connections_wait_count",
			Help:      "Total number of connections waited for",
		}),
		gaugeConnectionsWaitDuration: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "db_connections_wait_duration_seconds",
			Help:      "Total time blocked waiting for a new connection (seconds)",
		}),
		gaugeConnectionsMaxIdleClosed: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "db_connections_max_idle_closed",
			Help:      "Total number of connections closed due to SetMaxIdleConns",
		}),
		gaugeConnectionsMaxIdleTime: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "db_connections_max_idle_time_closed",
			Help:      "Total number of connections closed due to SetConnMaxIdleTime",
		}),
		gaugeConnectionsMaxLifetime: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "db_connections_max_lifetime_closed",
			Help:      "Total number of connections closed due to SetConnMaxLifetime",
		}),
	}

	// Collect immediately on startup.
	collector.collect()

	// Start background collection goroutine.
	go collector.run()

	return collector
}

// Stop halts the background collection goroutine and stops the ticker.
func (c *DBStatsCollector) Stop() {
	close(c.stop)
	c.ticker.Stop()
}

// run loops until Stop() is called, collecting stats on each tick.
func (c *DBStatsCollector) run() {
	for {
		select {
		case <-c.ticker.C:
			c.collect()
		case <-c.stop:
			return
		}
	}
}

// collect reads sql.DB.Stats() and updates all registered gauges.
func (c *DBStatsCollector) collect() {
	stats := c.db.Stats()

	c.gaugeConnectionsMax.Set(float64(stats.MaxOpenConnections))
	c.gaugeConnectionsOpen.Set(float64(stats.OpenConnections))
	c.gaugeConnectionsInUse.Set(float64(stats.InUse))
	c.gaugeConnectionsIdle.Set(float64(stats.Idle))
	c.gaugeConnectionsWaitCount.Set(float64(stats.WaitCount))
	c.gaugeConnectionsWaitDuration.Set(stats.WaitDuration.Seconds())
	c.gaugeConnectionsMaxIdleClosed.Set(float64(stats.MaxIdleClosed))
	c.gaugeConnectionsMaxIdleTime.Set(float64(stats.MaxIdleTimeClosed))
	c.gaugeConnectionsMaxLifetime.Set(float64(stats.MaxLifetimeClosed))
}

// Snapshot returns the current sql.DB.Stats() for one-off reads (e.g., health
// check endpoints or logging). It does not update Prometheus gauges.
func Snapshot(db *sql.DB) sql.DBStats {
	return db.Stats()
}

// LogStats logs the current database connection pool statistics. Useful for
// startup diagnostics or periodic health reporting.
func LogStats(db *sql.DB) {
	stats := db.Stats()
	slog.Info("DB stats",
		"open", stats.OpenConnections,
		"inUse", stats.InUse,
		"idle", stats.Idle,
		"maxOpen", stats.MaxOpenConnections,
		"waitCount", stats.WaitCount,
		"waitDuration", stats.WaitDuration,
	)
}
