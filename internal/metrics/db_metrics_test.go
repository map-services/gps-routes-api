package metrics

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

// openTestDB opens a temporary in-memory SQLite database for testing.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	assert.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestSnapshot(t *testing.T) {
	db := openTestDB(t)
	db.SetMaxOpenConns(5)

	stats := Snapshot(db)
	assert.Equal(t, 5, stats.MaxOpenConnections)
	assert.Equal(t, 0, stats.OpenConnections)
}

func TestLogStats(t *testing.T) {
	db := openTestDB(t)
	// LogStats should not panic
	assert.NotPanics(t, func() {
		LogStats(db)
	})
}

func TestDBStatsCollector_Lifecycle(t *testing.T) {
	db := openTestDB(t)
	db.SetMaxOpenConns(1)

	collector := NewDBStatsCollector(db, 10*time.Millisecond)
	assert.NotNil(t, collector)

	// Give the collector time to run at least one tick.
	time.Sleep(30 * time.Millisecond)

	// Stop should not panic.
	collector.Stop()

	// Verify stats are readable after stop.
	stats := Snapshot(db)
	assert.Equal(t, 1, stats.MaxOpenConnections)
}

func TestDBStatsCollector_CollectsStats(t *testing.T) {
	db := openTestDB(t)
	db.SetMaxOpenConns(3)

	// Ping to establish a connection.
	err := db.PingContext(t.Context())
	assert.NoError(t, err)

	stats := db.Stats()
	assert.Equal(t, 3, stats.MaxOpenConnections)
}
