# AGENTS.md — Build, Test & CI Guidelines for gps-routes-api

## Build Requirements

### CGO is mandatory
The project uses `github.com/mattn/go-sqlite3`, a **cgo binding** to the C SQLite
library. You **must** build with `CGO_ENABLED=1`:

```bash
CGO_ENABLED=1 go build -tags "sqlite,sqlite_fts5" ./...
CGO_ENABLED=1 go test -tags "sqlite,sqlite_fts5" ./...
```

- `CGO_ENABLED=0` will **fail** — go-sqlite3 has no pure-Go fallback in this project.
- On Ubuntu/Debian CI, `libsqlite3-dev` must be installed for the C toolchain.
- On macOS, the system C toolchain is sufficient (no Homebrew SQLite needed for
  `go-sqlite3` — it compiles its own amalgamation, but CGO must be enabled).

### Build tags
Always use: `-tags "sqlite,sqlite_fts5"`

- `sqlite` — selects go-sqlite3 as the driver (via build constraints in the
  `database/sql` import aliases).
- `sqlite_fts5` — enables the FTS5 full-text search virtual table (required for
  `routes_fts` queries).
- `sqlite_math_functions` is **NOT needed** — haversine is registered as a
  Go function via `ConnectHook`, avoiding the need for SQLite math functions.

## Spatial Queries (No Spatialite)

Spatialite has been **completely removed**. Spatial queries use:

1. **R*Tree bboxes** — SQLite's built-in `rtree` module. The `routes_rtree`
   virtual table has triggers on INSERT/UPDATE/DELETE that keep it in sync with
   `routes`. Bbox queries: `routes.rowid IN (SELECT id FROM routes_rtree WHERE ...)`.

2. **Haversine distance** — A Go-registered `haversine_distance(lon1, lat1, lon2, lat2)`
   SQL function, registered via the `sqlite3_haversine` driver's `ConnectHook`:
   ```go
   sql.Register("sqlite3_haversine", &sqlite3.SQLiteDriver{
       ConnectHook: func(conn *sqlite3.SQLiteConn) error {
           return conn.RegisterFunc("haversine_distance", haversineDistance, true)
       },
   })
   ```

3. **No `_geoloc` column** — The BLOB column has been removed from the schema.
   lat/lng are stored directly in `routes` and indexed via R*Tree.
   NOTE: `_geoloc` still appears as a JSON field name in `models/domain/`
   (`route_metadata.go`, `route_summary.go`) for GeoJSON serialization — this
   is a struct tag, not a DB column.

## Test Conventions

### Default Limit
`SearchHits` defaults `Limit` to 10 when `criteria.Limit` is 0 (otherwise SQLite
receives `LIMIT 0` and returns no rows). This is handled by `defaultLimit()` in
`repositories/sqlite.go`.

### R*Tree Coordinate Precision
SQLite's R*Tree quantizes coordinates slightly (e.g., `-0.13` may come back as
`-0.13000001` or `-0.12999999`). When comparing R*Tree values in tests, use
`1e-6` tolerance, not `1e-9`:
```go
func approxEq(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
```

### Test Database Setup
Tests use temp-file SQLite databases (not `:memory:`) when testing R*Tree and
FTS5 virtual tables, because `:memory:` databases have separate per-connection
schemas that can't share virtual tables across connections. The test helper
`setupSQLiteTestDB` creates a temp file DB with the full schema including
`routes_rtree` + sync triggers.

### FTS5 + R*Tree + bm25 Behavior
- The `routes_fts` join is only added when `criteria.Query != ""` (to avoid
  "no such table: routes_fts" errors).
- bm25 scores can be negative with weighted columns (`1.0, 0.4, 0.1`).
  The `BuildFTSSort` method returns `-bm25(...)` for proper best-first ordering.

## CI Pipeline (`.github/workflows/build.yml`)

The `build-and-test` job:
1. Checks out code
2. Sets up Go 1.27
3. Installs `libsqlite3-dev` (NOT `libsqlite3-mod-spatialite`)
4. Runs `make build` and `make test-ci`
5. Publishes JUnit test results
6. Uploads coverage to Coveralls
7. Runs **golangci-lint v2** (action v9)

### Lint Configuration
No `.golangci.yml` config file — uses golangci-lint v2 with **default linters**.
Known enforced linters include `errcheck` (all error return values must be checked,
including `rows.Scan`). Always run before pushing:
```bash
golangci-lint run --timeout 5m ./...
```

### Makefile Targets
- `build` — `CGO_ENABLED=1 go build -tags "sqlite,sqlite_fts5" -o gps-routes .`
- `build-static` — CGO-enabled with `-ldflags "-extldflags '-static'"` for true static linking
- `test-ci` — runs gotestsum with JUnit XML output for CI
- `test` — runs tests directly with `go test`

### docker-compose.yml
The `docker-compose.yml` no longer references `libsqlite3-mod-spatialite` or
Spatialite volumes. The only environment variable for SQLite is `SQLITE_DB_PATH`.

## Common Pitfalls

| Issue | Symptom | Fix |
|-------|---------|-----|
| CGO disabled | build fails / driver not found | `CGO_ENABLED=1` always |
| Missing build tags | FTS5/R*Tree features unavailable | `-tags "sqlite,sqlite_fts5"` |
| Spatialite references | CI build fails (no lib installed) | Use R*Tree + haversine instead |
| `LIMIT 0` | empty results from SearchHits | `defaultLimit()` adds fallback to 10 |
| R*Tree float comparison | test fails on exact match | use `1e-6` tolerance, not `1e-9` |
| `rows.Scan` unchecked | golangci-lint errcheck failure | always check error return |
