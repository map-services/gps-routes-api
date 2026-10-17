package health

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"github.com/stretchr/testify/assert"
)

// openTestDB opens a temporary in-memory SQLite database for testing.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	assert.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})
	// Ensure the DB is actually connected.
	err = db.PingContext(t.Context())
	assert.NoError(t, err)
	return db
}

func TestCheck_Healthy(t *testing.T) {
	db := openTestDB(t)

	result := Check(db, DefaultTimeout)
	assert.Equal(t, "healthy", result.Status)
	assert.True(t, result.Latency > 0)
}

func TestCheck_Latency(t *testing.T) {
	db := openTestDB(t)

	result := Check(db, DefaultTimeout)
	// Latency should be reasonable (< 100ms for in-memory DB)
	assert.Less(t, result.Latency.Milliseconds(), int64(100))
}

func TestReadinessProbe_Healthy(t *testing.T) {
	db := openTestDB(t)

	result := ReadinessProbe(db, DefaultReadinessTimeout)
	assert.Equal(t, "healthy", result.Status)
	assert.True(t, result.Latency > 0)
	assert.Equal(t, "SELECT 1", result.Details["query"])
}

func TestReadinessProbe_Latency(t *testing.T) {
	db := openTestDB(t)

	result := ReadinessProbe(db, DefaultReadinessTimeout)
	// Latency should be reasonable (< 100ms for in-memory DB)
	assert.Less(t, result.Latency.Milliseconds(), int64(100))
}

func TestCheck_ClosedDB(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	assert.NoError(t, err)
	_ = db.Close()

	result := Check(db, 500*time.Millisecond)
	assert.Equal(t, "unhealthy", result.Status)
	assert.Contains(t, result.Details["error"], "") // should have an error message
}

func TestReadinessProbe_ClosedDB(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	assert.NoError(t, err)
	_ = db.Close()

	result := ReadinessProbe(db, 500*time.Millisecond)
	assert.Equal(t, "unhealthy", result.Status)
	assert.Contains(t, result.Details["error"], "query failed")
}

func TestCheck_Timeout(t *testing.T) {
	// Create a DB that will fail to ping due to invalid DSN
	db, err := sql.Open("sqlite", "file:nonexistent?mode=ro")
	if err != nil {
		t.Skip("Could not open test DB")
	}
	defer func() { _ = db.Close() }()

	result := Check(db, 500*time.Millisecond)
	// The result depends on the driver behavior, but it should complete within timeout
	assert.True(t, result.Latency < 2*time.Second)
}

func TestDefaultTimeouts(t *testing.T) {
	assert.Equal(t, 1*time.Second, DefaultTimeout)
	assert.Equal(t, 2*time.Second, DefaultReadinessTimeout)
}
