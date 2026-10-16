.PHONY: all build build-sqlite build-static test test-ci test-race lint clean run run-sqlite import-data docker docker-build docker-push help

# Default target
all: build test

# --- Build targets ---

# Build the application (pure Go SQLite via modernc.org/sqlite, no CGO).
build: build-sqlite

build-sqlite:
	go build -o gps-routes .

# Build a statically-linked binary (pure Go — no CGO needed).
build-static:
	CGO_ENABLED=0 go build -ldflags="-w -s" -o gps-routes .

# --- Run targets ---

# Run the API server (binds to port 8080 by default).
run: run-sqlite

run-sqlite:
	go run . api-server 8080

# Import JSON route data into ./data/routes.db. Creates the data/ directory
# if it doesn't exist; SQLITE_DB_PATH defaults to data/routes.db.
# Import data into ./data/routes.db. Pass OVERWRITE=1 to delete the db file first.
import-data:
	mkdir -p ./data
	go run . import $(or $(DATA_PATH),data/backup)$(if $(filter 1,$(OVERWRITE)), --overwrite,)

# --- Test targets ---

# Run all tests with SQLite + FTS5.
test:
	go test -v -coverprofile=profile.cov -coverpkg=./... ./...

# Run tests with race detector (slow; use for concurrency debugging).
test-race:
	go test -race -count=1 ./...

# Run tests suitable for CI: JUnit XML output + race detector.
test-ci:
	mkdir -p ./test-reports/
	gotestsum --junitfile=./test-reports/junit.xml --format github-actions \
		-- -v -race -coverprofile=profile.cov -coverpkg=./... ./...


# --- Lint ---

# Note: golangci-lint requires compatible Go toolchain. If you see
# "export data version X is greater than maximum" errors, use `go vet` instead.
lint:
	golangci-lint run || go vet ./...

# --- Docker targets ---

docker: docker-build

docker-build:
	docker build -t gps-routes-api .

docker-run:
	docker run --rm -p 8080:8080 \
		-e SQLITE_DB_PATH=/app/data/routes.db \
		-e GPS_ROUTES_API_KEY=your_key \
		gps-routes-api

docker-push: docker-build
	docker tag gps-routes-api ghcr.io/map-services/gps-routes-api:latest
	docker push ghcr.io/map-services/gps-routes-api:latest

# --- Cleanup ---

clean:
	rm -f gps-routes profile.cov
	rm -rf ./test-reports/

# --- Help ---

help:
	@echo "gps-routes-api Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all          Build and test (default)"
	@echo "  build        Build with SQLite + FTS5 (pure Go, no CGO)"
	@echo "  build-static Build a statically-linked binary (pure Go)"
	@echo "  run          Run the API server on port 8080"
	@echo "  import-data  Import JSON route metadata into ./data/routes.db (defaults to data/backup; set OVERWRITE=1 to delete db first; override input with DATA_PATH)"
	@echo "  test         Run all tests with coverage"
	@echo "  test-ci      Run tests with junit XML + race detector for CI"
	@echo "  test-race    Run tests with race detector (slow)"
	@echo "  lint         Run golangci-lint (falls back to go vet)"
	@echo "  docker       Build Docker image"
	@echo "  docker-run   Run the Docker container"
	@echo "  docker-push  Build and push to ghcr.io"
	@echo "  clean        Remove build artifacts"
	@echo "  help         Show this help message"
