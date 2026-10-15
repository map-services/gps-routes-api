package health

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CheckResult represents the result of a health check.
type CheckResult struct {
	Status  string            `json:"status"`
	Latency time.Duration     `json:"latency"`
	Details map[string]string `json:"details,omitempty"`
}

// Check performs a database-agnostic health check using db.PingContext with a
// timeout. It returns a CheckResult indicating whether the database is healthy.
func Check(db *sql.DB, timeout time.Duration) CheckResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := db.PingContext(ctx)
	latency := time.Since(start)

	if err != nil {
		return CheckResult{
			Status:  "unhealthy",
			Latency: latency,
			Details: map[string]string{
				"error": err.Error(),
			},
		}
	}

	return CheckResult{
		Status:  "healthy",
		Latency: latency,
	}
}

// ReadinessProbe performs a readiness check by executing a trivial SELECT 1
// query. This is a more thorough check than PingContext as it verifies the
// database can actually execute queries, not just accept connections.
func ReadinessProbe(db *sql.DB, timeout time.Duration) CheckResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var result int
	err := db.QueryRowContext(ctx, "SELECT 1").Scan(&result)
	latency := time.Since(start)

	if err != nil {
		return CheckResult{
			Status:  "unhealthy",
			Latency: latency,
			Details: map[string]string{
				"error": fmt.Sprintf("query failed: %v", err),
			},
		}
	}

	return CheckResult{
		Status:  "healthy",
		Latency: latency,
		Details: map[string]string{
			"query": "SELECT 1",
		},
	}
}

// DefaultTimeout is the default timeout for health checks (1 second).
const DefaultTimeout = 1 * time.Second

// DefaultReadinessTimeout is the default timeout for readiness probes (2 seconds).
const DefaultReadinessTimeout = 2 * time.Second
