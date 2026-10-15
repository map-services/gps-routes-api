package metrics

import (
	"database/sql"
	"log"
	"time"

	"github.com/Depado/ginprom"
)

// DBStatsCollector periodically collects sql.DB.Stats() and exposes them
// as Prometheus gauges via ginprom. It is database-agnostic and works with
// any driver that implements *sql.DB, including SQLite.
type DBStatsCollector struct {
	db         *sql.DB
	prometheus *ginprom.Prometheus
	ticker     *time.Ticker
	stop       chan struct{}
}

// NewDBStatsCollector creates a new collector that registers gauges for
// database connection pool statistics and starts a background goroutine to
// update them at the given interval. Call Stop() when the server shuts down.
func NewDBStatsCollector(db *sql.DB, prometheus *ginprom.Prometheus, interval time.Duration) *DBStatsCollector {
	// Register gauges for all DB stats metrics.
	prometheus.AddCustomGauge("db_connections_max", "Maximum number of open connections to the database", []string{})
	prometheus.AddCustomGauge("db_connections_open", "Number of open connections to the database", []string{})
	prometheus.AddCustomGauge("db_connections_in_use", "Number of connections currently in use", []string{})
	prometheus.AddCustomGauge("db_connections_idle", "Number of idle connections to the database", []string{})
	prometheus.AddCustomGauge("db_connections_wait_count", "Total number of connections waited for", []string{})
	prometheus.AddCustomGauge("db_connections_wait_duration_seconds", "Total time blocked waiting for a new connection (seconds)", []string{})
	prometheus.AddCustomGauge("db_connections_max_idle_closed", "Total number of connections closed due to SetMaxIdleConns", []string{})
	prometheus.AddCustomGauge("db_connections_max_idle_time_closed", "Total number of connections closed due to SetConnMaxIdleTime", []string{})
	prometheus.AddCustomGauge("db_connections_max_lifetime_closed", "Total number of connections closed due to SetConnMaxLifetime", []string{})

	collector := &DBStatsCollector{
		db:         db,
		prometheus: prometheus,
		ticker:     time.NewTicker(interval),
		stop:       make(chan struct{}),
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

	c.prometheus.SetGaugeValue("db_connections_max", []string{}, float64(stats.MaxOpenConnections))        //nolint:errcheck
	c.prometheus.SetGaugeValue("db_connections_open", []string{}, float64(stats.OpenConnections))          //nolint:errcheck
	c.prometheus.SetGaugeValue("db_connections_in_use", []string{}, float64(stats.InUse))                  //nolint:errcheck
	c.prometheus.SetGaugeValue("db_connections_idle", []string{}, float64(stats.Idle))                     //nolint:errcheck
	c.prometheus.SetGaugeValue("db_connections_wait_count", []string{}, float64(stats.WaitCount))          //nolint:errcheck
	c.prometheus.SetGaugeValue("db_connections_wait_duration_seconds", []string{}, stats.WaitDuration.Seconds()) //nolint:errcheck
	c.prometheus.SetGaugeValue("db_connections_max_idle_closed", []string{}, float64(stats.MaxIdleClosed)) //nolint:errcheck
	c.prometheus.SetGaugeValue("db_connections_max_idle_time_closed", []string{}, float64(stats.MaxIdleTimeClosed)) //nolint:errcheck
	c.prometheus.SetGaugeValue("db_connections_max_lifetime_closed", []string{}, float64(stats.MaxLifetimeClosed)) //nolint:errcheck
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
	log.Printf("DB stats: open=%d, inUse=%d, idle=%d, maxOpen=%d, waitCount=%d, waitDuration=%s",
		stats.OpenConnections, stats.InUse, stats.Idle, stats.MaxOpenConnections,
		stats.WaitCount, stats.WaitDuration)
}
