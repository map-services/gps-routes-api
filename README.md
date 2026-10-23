# gps-routes-api

[![Go Report Card](https://goreportcard.com/badge/github.com/map-services/gps-routes-api)](https://goreportcard.com/report/github.com/map-services/gps-routes-api)
[![Coverage Status](https://coveralls.io/repos/github/map-services/gps-routes-api/badge.svg)](https://coveralls.io/github/map-services/gps-routes-api)

REST API for searching, filtering, and retrieving GPS walking routes. Powered by
Go, SQLite (CGO + FTS5 + R*Tree), and a Go-registered Haversine function for
geospatial queries — **no Spatialite extension required**.

## API Overview

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/v1/gps-routes/search` | Search by text query, bounding box, nearby radius, facets, and/or distance sorting |
| `GET`  | `/v1/gps-routes/{objectID}` | Retrieve a single route by ID |
| `GET`  | `/healthz` | Health-check endpoint |

### Search Request Example

```bash
curl -X POST http://localhost:8080/v1/gps-routes/search \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your_api_key_here" \
  -d '{
    "query": "canal walking",
    "boundingBox": [-2.98, 54.41, -2.94, 54.43],
    "nearby": {"center": {"latitude": 51.51, "longitude": -0.13}, "distanceKm": 100},
    "facets": {"activities": ["walking"]},
    "limit": 25
  }'
```

### Query Syntax

The `query` parameter uses SQLite FTS5 full-text search. It searches three
columns with different relevance weights:

| Column | Weight | Relevance |
|--------|--------|-----------|
| `title` | A | Highest |
| `display_address` | B | Medium |
| `description` | C | Lowest |

**Every query is automatically converted to a prefix search.** Each word gets
a wildcard (`*`) appended, so `"canal walk"` matches "canal walk", "canal
walking", "canal walker", etc.

**Supported FTS5 operators:**

| Syntax | Description | Example |
|--------|-------------|---------|
| Plain words | Matches any word (prefix). Words are ANDed together. | `"canal walking"` finds routes containing both "canal*" and "walking*" |
| `"quoted phrase"` | Exact phrase match (no prefix expansion) | `"river walk"` matches the exact phrase |
| `NEAR/A` | Words within A tokens of each other | `"canal" NEAR/5 "bridge"` |
| `-term` | Exclude routes containing the term | `"walking -cycling"` |
| `term1 OR term2` | Match either term | `"river OR canal"` |
| `*` | Explicit wildcard (also added automatically) | `"canal*"` matches "canal", "canals", etc. |

**Examples:**

```json
{"query": "river walk"}
```
Matches routes containing words starting with "river" AND "walk" (prefix search).

```json
{"query": "\"coast path\""}
```
Matches the exact phrase "coast path" (no prefix expansion).

```json
{"query": "walking -cycling"}
```
Matches routes about walking but excludes those mentioning cycling.

```json
{"query": "river NEAR/5 bridge"}
```
Matches routes where "river" and "bridge" appear within 5 words of each other.
```

## Local Development

### Prerequisites

- **Go 1.27+** ([install](https://go.dev/dl/))
- **C compiler** (gcc/clang) — required because `github.com/mattn/go-sqlite3` is a cgo binding
  - macOS: `xcode-select --install`
  - Ubuntu/Debian: `sudo apt-get install build-essential libsqlite3-dev`

### Quick Start

```bash
# 1. Clone & configure
git clone https://github.com/map-services/gps-routes-api.git
cd gps-routes-api
cp .env.example .env
# Edit .env if needed (defaults are fine for development)

# 2. Build the binary (CGO + FTS5, all flags handled by Makefile)
make build

# 3. Run the API server on port 8080
make run

# 4. (Optional) Load sample data into ./data/routes.db
make import-data
# Then restart: make run
```

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `GPS_ROUTES_API_KEY` | Yes | — | API key sent via `X-API-Key` header |
| `SQLITE_DB_PATH` | No | `./data/routes.db` | Path to the SQLite database |
| `OS_NAMES_API_KEY` | No | — | API key for OSM names lookup (faceted search) |
| `SEARCH_TIMEOUT_SECONDS` | No | `10` | Search query timeout in seconds |

## Build Commands

All build targets are in the [Makefile](Makefile). Key targets:

```bash
make build        # Build binary with SQLite + FTS5 (CGO required)
make run          # Build and run the API server on port 8080
make import-data  # Import sample JSON data into ./data/routes.db
make test         # Run all tests with coverage
make lint         # Run golangci-lint (falls back to go vet)
make docker       # Build Docker image
make help         # Show all Makefile targets
```

> **Important:** Always use `CGO_ENABLED=1` and `-tags "sqlite,sqlite_fts5"`.
> Without CGO, the go-sqlite3 driver will not compile.

## Docker

```bash
docker build -t gps-routes-api .
docker run --rm -p 8080:8080 \
  -e GPS_ROUTES_API_KEY=your_key_here \
  gps-routes-api
```

The Docker image ships with a **pre-seeded database** built from the sample data
in `data/backup/` (7,069 routes). To use your own database, bind-mount a file:

```bash
docker run --rm -p 8080:8080 \
  -e GPS_ROUTES_API_KEY=your_key_here \
  -v $(pwd)/my-routes.db:/app/data/routes.db \
  gps-routes-api
```

## Architecture

```
main.go                    # CLI entry point — "api-server" or "import" subcommands
cmds/                      # Command implementations (server setup, data import, sitemap)
├── http_api_server.go     # Gin HTTP router + handlers
├── import_data.go         # JSON → SQLite bulk import
├── setup_sqlite_db.go     # Schema creation + extension detection
├── sitemap.go             # XML sitemap generation
db/                        # Database abstraction layer
├── dialect.go             # SQL dialect interface + SQLite implementation
├── query_builder.go       # Query construction (FTS, facets, bbox, nearby, sorting)
├── sqlite_connect.go      # SQLite driver registration (FTS5 + R*Tree + haversine)
└── migrations/            # SQL schema scripts
repositories/              # Data access layer
├── sqlite.go              # SQLiteDbRepository (Store, SearchHits, CountAll, etc.)
models/                    # Domain models + API request/response types
middlewares/               # Auth middleware (API key validation)
services/                  # External service clients (e.g., OS Data Hub)
routes/                    # HTTP route definitions
```

### Spatial Queries (No Spatialite)

All geospatial functionality uses **built-in SQLite features**:

- **R*Tree bbox filtering**: A `routes_rtree` virtual table (SQLite's built-in `rtree`
  module) indexes latitude/longitude with INSERT/UPDATE/DELETE triggers on the `routes`
  table. Used by `BuildSTWithinQuery` for bounding-box queries.

- **Haversine distance**: A Go-registered `haversine_distance(lon1, lat1, lon2, lat2)`
  SQL scalar function, attached to each connection via `mattn/go-sqlite3`'s
  `ConnectHook`. Used by `BuildSTDWithinQuery` (radius search) and
  `BuildDistanceSort` (nearest-first ordering).

- **No `_geoloc` column**: The old Spatialite `GEOMETRY(Point, 4326)` column has been
  removed. Lat/lng are stored in plain `REAL` columns and indexed directly.

## Data Model

Routes are stored in SQLite with this schema (see `db/migrations/00002_sqlite_schema.sql`):

```sql
CREATE TABLE routes (
    object_id   TEXT PRIMARY KEY,
    latitude    REAL,        -- Start position latitude
    longitude   REAL,        -- Start position longitude
    distance_km REAL,
    title       TEXT,
    description TEXT,
    -- ... + JSON-array columns (terrain, activities, facilities, etc.)
    -- + FTS5 virtual table (routes_fts) for full-text search
    -- + R*Tree virtual table (routes_rtree) for bbox filtering
);
```

Sample data: 7,069 routes from `data/backup/` (16 JSON files, hex-named buckets 0–f).

## Testing

```bash
make test        # Run tests with coverage
make test-race   # Run tests with race detector (slow)
make test-ci     # CI mode: JUnit XML + race detector
make lint        # golangci-lint (falls back to go vet)
```

Tests use temporary SQLite database files (not `:memory:`) to properly exercise
R*Tree virtual tables and FTS5. See [`AGENTS.md`](AGENTS.md) for detailed build/test guidance.

## CI/CD

GitHub Actions (`build.yml`) runs on every PR:
- Builds with `make build`
- Runs tests with `make test-ci` (JUnit XML + race detector)
- Publishes test results to the PR
- Uploads coverage to Coveralls
- Runs golangci-lint v2
- Builds & pushes Docker image to `ghcr.io` (on main + tags)

## Documentation

- [`doc/SQLITE_CGO_BUILD_GUIDE.md`](doc/SQLITE_CGO_BUILD_GUIDE.md) — CGo setup and troubleshooting
- [`AGENTS.md`](AGENTS.md) — Build, test, and lint guidelines for contributors
- [`openapi-spec/gps-routes.yaml`](openapi-spec/gps-routes.yaml) — OpenAPI 3.0 spec

## License

MIT
