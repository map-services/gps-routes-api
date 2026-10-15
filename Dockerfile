# syntax=docker/dockerfile:1
#
# GPS Routes API — SQLite + FTS5 build.
#
#   - Build:  CGO_ENABLED=1 with -tags "sqlite,sqlite_fts5" (go-sqlite3 is cgo).
#   - Spatial queries: SQLite's built-in R*Tree virtual table (routes_rtree)
#     provides bbox filtering; a Go-registered haversine_distance() function
#     provides radius filtering — NO Spatialite extension required.
#   - Seed: the sample `data/routes.db` is (re)built at BUILD time from the
#     tracked `data/backup/` JSON set (schema + 7069 routes + R*Tree + FTS5).
#     A fresh container therefore returns real data on the first request instead
#     of the "no such table: routes" 500 that a bare NewSQLiteDB startup produces.
#   - Override: bind-mount your own `data/routes.db` over `./data/routes.db` to
#     run with production data (mount hides the baked sample; the binary then
#     opens your file verbatim with no reseed).
#
# Build:  docker build -t gps-routes-api:sqlite .
# Run:    docker run --rm -p 8080:8080 \
#           -e GPS_ROUTES_API_KEY=<your-key> \
#           gps-routes-api:sqlite
# Query:  curl -X POST http://localhost:8080/v1/gps-routes/search \
#           -H "Content-Type: application/json" \
#           -H "X-API-Key: <your-key>" \
#           -d '{"boundingBox":[-2.984805,54.418630,-2.947941,54.438327]}'
#
# Required runtime env: GPS_ROUTES_API_KEY  (app panics without it).
# Optional: OS_NAMES_API_KEY (OSM names lookup). SQLITE_DB_PATH defaults to
#   /app/data/routes.db and may be bind-mounted to supply your own database.
# To publish an amd64 image, add a CI job like the existing docker-publish job
# with `platforms: linux/amd64` on ubuntu-latest (native cgo, no cross toolchain).

# ---------------------------------------------------------------------------
# 1) Build stage: compile + seed the database
# ---------------------------------------------------------------------------
# golang:1.27 is Debian(bookworm)-based => glibc toolchain required for cgo and
# for go-sqlite3 compilation.
FROM golang:1.27 AS build

ENV CGO_ENABLED=1

WORKDIR /src

# libsqlite3-dev is required to CGO-compile github.com/mattn/go-sqlite3.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Compile with the sqlite build tags (FTS5 via sqlite_fts5 tag; R*Tree and
# haversine_distance() are Go-registered, no external extension).
RUN go build -tags "sqlite,sqlite_fts5" -ldflags="-w -s" -o /gps-routes .

# Seed /app/data/routes.db from the bundled sample backup.
# setupSQLiteDB recreates the schema (00002_sqlite_schema.sql, incl.
# routes_fts + routes_rtree); Store writes lat/lng and the R*Tree trigger
# auto-populates bbox entries.
RUN mkdir -p /src/data && \
    SQLITE_DB_PATH=/src/data/routes.db /gps-routes import /src/data/backup

# ---------------------------------------------------------------------------
# 2) Runtime stage: minimal image, ships the pre-seeded DB
# ---------------------------------------------------------------------------
FROM ubuntu:24.04 AS runtime

ENV TZ=UTC \
    GIN_MODE=release \
    SQLITE_DB_PATH=/app/data/routes.db

# curl is used by HEALTHCHECK.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        libsqlite3-dev \
        passwd \
        tzdata \
    && rm -rf /var/lib/apt/lists/* && \
    update-ca-certificates

RUN useradd --no-create-home --shell /usr/sbin/nologin appuser

WORKDIR /app
RUN mkdir -p /app/data && chown appuser:appuser /app/data

# Binary + pre-seeded database (bind-mount to override with your own).
COPY --from=build --chown=appuser:appuser /gps-routes          /app/gps-routes
COPY --from=build --chown=appuser:appuser /src/data/routes.db   /app/data/routes.db

USER appuser

EXPOSE 8080/tcp

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -sf "http://localhost:8080/healthz" || exit 1

# `api-server <port>`; CMD supplies the default port.
ENTRYPOINT ["./gps-routes", "api-server"]
CMD ["8080"]
