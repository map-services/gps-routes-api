# SQLite3 CGo Compilation Guide

**Date:** 12 April 2026
**Purpose:** Document CGo setup and troubleshoot common issues
**Status:** Validated on macOS ✅

---

## ⚠️ IMPORTANT: FTS5 Build Tag Required

**For full-text search functionality**, you MUST build with the FTS5 extension enabled:

```bash
# Build with FTS5 support (recommended for full functionality)
CGO_ENABLED=1 go build -tags="sqlite,sqlite_fts5" -o app .

# Or for development:
CGO_ENABLED=1 go run -tags="sqlite,sqlite_fts5" . migrate-data --dry-run
```

**Without FTS5**, the migration tool will automatically fall back to a basic schema without full-text search capabilities. This allows the migration to proceed but with reduced search functionality.

**Without this tag**, you'll get: `⚠️ FTS5 not available, creating schema without full-text search` and the migration will continue with basic functionality.

---

## Quick Start

### macOS (Xcode/Homebrew)

```bash
# Already have: gcc (via Xcode Command Line Tools)
# Verify:
gcc --version  # Should show >= 11.x

# Install sqlite3 headers if needed:
brew install sqlite3

# Build with SQLite3:
CGO_ENABLED=1 go build -o app .
```

### Linux (Ubuntu/Debian)

```bash
# Install build essentials and sqlite3 dev headers:
sudo apt-get update
sudo apt-get install -y build-essential sqlite3 libsqlite3-dev

# Build:
CGO_ENABLED=1 go build -o app .
```

### Docker (Multi-stage Build)

```dockerfile
FROM golang:1.23-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY . .
ENV CGO_ENABLED=1
RUN go build -o app .

FROM alpine:latest
RUN apk add --no-cache sqlite-libs
COPY --from=builder /app/app /app
ENTRYPOINT ["/app"]
```

---

## Verification Steps

### 1. Verify CGo Works

```bash
# Check go-sqlite3 module loads
go test -v github.com/mattn/go-sqlite3

# Expected output:
# PASS
# ok      github.com/mattn/go-sqlite3     0.388s
```

### 2. Verify Compiler Available

```bash
which gcc
gcc --version
```

### 3. Quick in-memory DB test

```go
package main

import (
    "database/sql"
    _ "github.com/mattn/go-sqlite3"
)

func main() {
    db, _ := sql.Open("sqlite3", ":memory:")
    db.Query("SELECT 1 AS num")
}

// Compile with: CGO_ENABLED=1 go run main.go
```

---

## Build Flags for Extensions

### FTS5 (Full-Text Search)

**Why:** Standard SQLite doesn't include FTS5; add to enable advanced search.

```bash
# Flags: sqlite (vendored SQLite amalgamation via CGO), sqlite_fts5 (enable FTS5)
CGO_ENABLED=1 go build \
  -tags="sqlite,sqlite_fts5" \
  -o app .
```

**Impact:**

- Binary size: +2 MB (minimal)
- Build time: +30 seconds
- Runtime perf: Negligible overhead

### R*Tree & Haversine (Geospatial Queries)

**Why:** Spatial queries (bbox filtering + radius/distance calculations) are
handled WITHOUT the `libspatialite` extension:

- **R*Tree**: SQLite's built-in `rtree` virtual table — compiled in when the
  `sqlite` build tag is set (uses the vendored SQLite amalgamation).
- **haversine_distance()**: A Go-registered scalar function attached to each
  SQLite connection via `mattn/go-sqlite3`'s `ConnectHook`. Implemented in
  `db/sqlite_connect.go`.

**No external packages required** — no `libspatialite-dev`, no `mod_spatialite.so`,
no `load_extension()` calls. The `sqlite` build tag (which enables CGO compilation
of the SQLite amalgamation) provides both R*Tree and the foundation for custom
function registration.

**Verify R*Tree is usable:**

```sql
CREATE VIRTUAL TABLE t USING rtree(id, min_x, max_x, min_y, max_y);
```

**Verify haversine_distance is registered:**

```sql
SELECT haversine_distance(-0.1278, 51.5074, -74.0060, 40.7128);  -- London→NYC ~5,570 km
```

---

## Common Issues & Fixes

### Issue: "error: command not found: gcc"

**Cause:** C compiler not installed
**Fix (macOS):**

```bash
xcode-select --install
# Or: brew install gcc
```

**Fix (Linux):**

```bash
sudo apt-get install build-essential
```

### Issue: "sqlite3.h: No such file or directory"

**Cause:** SQLite dev headers not installed
**Fix (macOS):**

```bash
brew install sqlite3
export CGO_CFLAGS="-I/opt/homebrew/opt/sqlite/include"
export CGO_LDFLAGS="-L/opt/homebrew/opt/sqlite/lib"
go build -o app .
```

**Fix (Linux):**

```bash
sudo apt-get install libsqlite3-dev
```

### Issue: "undefined reference to 'sqlite3_open'"

**Cause:** SQLite library not linked
**Fix:**

```bash
export CGO_LDFLAGS="-lsqlite3"
CGO_ENABLED=1 go build -o app .
```

### Issue: "text encoding: UCS2 not supported"

**Cause:** Rare SQLite encoding issue
**Fix:** Use newer sqlite3 version

```bash
brew upgrade sqlite3
# Verify newly installed version is used:
which sqlite3
sqlite3 --version
```

---

## Environment Variables

### Development Setup

**macOS (add to ~/.zshrc or ~/.bash_profile):**

```bash
export CGO_ENABLED=1
export CGO_CFLAGS="-I$(brew --prefix sqlite3)/include"
export CGO_LDFLAGS="-L$(brew --prefix sqlite3)/lib"
```

**Linux (add to ~/.bashrc):**

```bash
export CGO_ENABLED=1
# sqlite3-dev package installs to standard locations
```

### CI/CD Builds

```bash
# GitHub Actions
env:
  CGO_ENABLED: 1

# GitLab CI
script:
  - CGO_ENABLED=1 go build -o app .
```

---

## Performance Tuning

### Build Size Optimization

For production Docker images, strip symbols:

```bash
CGO_ENABLED=1 go build \
  -ldflags="-s -w" \
  CGO_ENABLED=1 go build -tags="sqlite,sqlite_fts5" -ldflags="-s -w" -o app .

# Result: ~8 MB binary (vs 15 MB with symbols)
```

### SQLite Runtime Pragmas

Add to connection initialization:

```go
pragmas := []string{
    "PRAGMA foreign_keys = ON",
    "PRAGMA journal_mode = WAL",
    "PRAGMA synchronous = NORMAL",
    "PRAGMA cache_size = 10000",
    "PRAGMA temp_store = MEMORY",
}
for _, pragma := range pragmas {
    db.Exec(pragma)
}
```

---

## Testing the Full Stack

### Manual Test

```bash
# 1. Build with FTS5 support
CGO_ENABLED=1 go build -tags="sqlite,sqlite_fts5" -o app .

# 2. Run the tests
go test -v ./...

# 3. Spin up the API
./app

# 4. Test the endpoint
curl http://localhost:8080/v1/gps-routes/search \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"query":"hiking"}'
```

### Docker Build Test

```bash
# Build image
docker build -t gps-routes-api:latest .

# Run container
docker run -p 8080:8080 gps-routes-api:latest

# Test
curl http://localhost:8080/v1/gps-routes/ref-data
```

---

## Platform-Specific Notes

### macOS (Apple Silicon / M1/M2)

**Native support:** ✅ Yes (GOARCH=arm64)

```bash
# Build for Apple Silicon:
export GOOS=darwin
export GOARCH=arm64
CGO_ENABLED=1 go build -o app .
```

### macOS (Intel)

**Native support:** ✅ Yes (GOARCH=amd64)

```bash
export GOOS=darwin
export GOARCH=amd64
CGO_ENABLED=1 go build -o app .
```

### Linux (x86_64)

**Native support:** ✅ Yes

```bash
export GOOS=linux
export GOARCH=amd64
CGO_ENABLED=1 go build -o app .
```

### Windows

**Support:** ⚠️ Limited (requires MinGW or MSVC)

```powershell
# Using MSVC (Visual Studio)
$env:CGO_ENABLED = "1"
go build -o app.exe .
```

---

## Troubleshooting Checklist

- [ ] CGo enabled: `echo $CGO_ENABLED` → should show `1`
- [ ] Compiler present: `which gcc` → shows path
- [ ] Compiler version: `gcc --version` → shows >= 11.x
- [ ] SQLite headers: Try `#include <sqlite3.h>` in C source
- [ ] SQLite lib: `find /usr -name "libsqlite3*"` → shows files
- [ ] go-sqlite3 imported: `grep "_ \"github.com/mattn/go-sqlite3\"" **/*.go`
- [ ] Test compiles: `go build -v ./...` → no errors
- [ ] Test runs: `go test ./...` → all tests pass

---

## Next Steps for Phase 4

When implementing the query layer (Phase 4), ensure:

1. **Build tag in Makefile:**

    ```makefile
    build:
        CGO_ENABLED=1 go build -tags="sqlite,sqlite_fts5" -o bin/app .
    ```

2. **Document in README:**
    - System packages required
    - CGo environment setup
    - Build instructions with flags

3. **CI/CD integration:**
    - Add CGO_ENABLED=1 to all build jobs
    - Test on macOS, Linux, Windows (if applicable)

4. **Docker optimization:**
    - Multi-stage build (compile in full-featured stage, minimal runtime stage)
    - Include sqlite3 runtime library in final image

---

**Document Version:** 1.0
**Last Updated:** 12 April 2026
**Status:** Validated and ready for Phase 4 implementation
