# syntax=docker/dockerfile:1
#
# GPS Routes API — SQLite + FTS5 + Spatialite build.
#
#   - Build:  CGO_ENABLED=1 with -tags "sqlite,sqlite_fts5" (go-sqlite3 is cgo).
#   - Spatialite: libsqlite3-mod-spatialite provides mod_spatialite.so, which the
#     `sqlite3_spatialite` driver (db/sqlite_connect.go init()) loads on first
#     Ping() so bbox predicates (ST_Within/BuildMbr) and _geoloc writes work.
#   - Seed: the sample `data/routes.db` is (re)built at BUILD time from the
#     tracked `data/backup/` JSON set (schema + 7069 routes + _geoloc + FTS5).
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
# for go-sqlite3's load_extension.
FROM golang:1.27 AS build

ENV CGO_ENABLED=1

WORKDIR /src

# libsqlite3-dev is required to CGO-compile github.com/mattn/go-sqlite3;
# libsqlite3-mod-spatialite is required BOTH so the build-time seed import can
# write _geoloc via ST_GeomFromText AND so spatialite functions resolve during
# the seed.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        libsqlite3-dev \
        libsqlite3-mod-spatialite \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Compile with the sqlite build tags (FTS5 + Spatialite via sqlite3_spatialite driver).
RUN go build -tags "sqlite,sqlite_fts5" -ldflags="-w -s" -o /gps-routes .

# Seed /app/data/routes.db from the bundled sample backup.
# setupSQLiteDB recreates the schema (00002_sqlite_schema.sql, incl. routes_fts);
# Store writes _geoloc via ST_GeomFromText(?,4326) => every route is bbox-queryable.
RUN mkdir -p /src/data && \
    SQLITE_DB_PATH=/src/data/routes.db /gps-routes import /src/data/backup

# ---------------------------------------------------------------------------
# 2) Runtime stage: minimal image, ships the pre-seeded DB + mod_spatialite
# ---------------------------------------------------------------------------
FROM ubuntu:24.04 AS runtime

ENV TZ=UTC \
    GIN_MODE=release \
    SQLITE_DB_PATH=/app/data/routes.db

# Runtime needs mod_spatialite.so (loaded by sqlite3_spatialite driver on Ping)
# so bbox predicates work against the seeded DB. curl is used by HEALTHCHECK.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        libsqlite3-mod-spatialite \
        passwd \
        tzdata \
    && rm -rf /var/lib/apt/lists/* && \
    update-ca-certificates

RUN useradd --no-create-home --shell /usr/sbin/nologin appuser

WORKDIR /app
RUN mkdir -p /app/data && chown appuser:appuser /app/data

# Binary + pre-seeded sample database (bind-mount to override with your own).
COPY --from=build --chown=appuser:appuser /gps-routes          /app/gps-routes
COPY --from=build --chown=appuser:appuser /src/data/routes.db   /app/data/routes.db

USER appuser

EXPOSE 8080/tcp

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -sf "http://localhost:8080/healthz" || exit 1

# `api-server <port>`; CMD supplies the default port.
ENTRYPOINT ["./gps-routes", "api-server"]
CMD ["8080"]
