# Benchmark Results

Baseline performance measurements for SQLite query patterns used by the GPS Routes API.

## Environment

- **CPU**: Apple M2 Pro
- **Go version**: 1.27
- **Driver**: `mattn/go-sqlite3` (CGO, identified as the **mattn** variant)
- **Build flags**: `CGO_ENABLED=1 -tags "sqlite,sqlite_fts5"`
- **Dataset**: Synthetic routes spread across the UK
- **Seed**: 42 (deterministic for reproducibility)

## 500 Routes

| Benchmark | ns/op | B/op | allocs/op | Description |
|-----------|-------|------|-----------|-------------|
| SearchSimple | 42,367 | 7,492 | 163 | Basic search with LIMIT |
| SearchFTS | 362,409 | 8,340 | 177 | FTS5 full-text search with bm25 sorting |
| SearchBbox | 24,272 | 5,035 | 94 | R*Tree bounding box query |
| SearchRadius | 32,461 | 6,140 | 130 | R*Tree prefilter + haversine distance |
| SearchFTSWithBbox | 209,963 | 8,909 | 181 | Combined FTS + R*Tree bbox |
| SearchWithFacets | 78,810 | 8,674 | 187 | JSON array overlap + scalar facet filter |
| FacetCounts | 162,168 | 2,057 | 45 | json_each unnested array counts |
| FacetCountsScalar | 117,311 | 2,857 | 81 | Scalar field counts |
| CountAll | 3,295 | 920 | 19 | Simple COUNT(*) |
| CountAllFTS | 45,282 | 1,240 | 29 | COUNT(*) with FTS filter |
| StoreBatch/10routes | 2,015,411 | 56,362 | 1,114 | Bulk insert of 10 routes |
| StoreBatch/100routes | 5,084,512 | 386,487 | 9,126 | Bulk insert of 100 routes |
| HaversineFunction | 2,799 | 784 | 22 | Raw haversine_distance() call |
| HaversineOverTable | 2,274 | 432 | 7 | Full-table haversine sort |

## 5000 Routes (Scaling)

| Benchmark | ns/op | B/op | allocs/op | Description |
|-----------|-------|------|-----------|-------------|
| SearchLargeDataset/Simple | 230,185 | 7,492 | 163 | Basic search with LIMIT |
| SearchLargeDataset/FTS | 2,428,085 | 8,320 | 176 | FTS5 full-text search with bm25 sorting |
| SearchLargeDataset/Bbox | 697,664 | 8,143 | 171 | R*Tree bounding box query |
| SearchLargeDataset/Radius | 167,974 | 42,149 | 1,466 | R*Tree prefilter + haversine distance |
| SearchLargeDataset/FTS+Bbox | 1,768,740 | 8,897 | 181 | Combined FTS + R*Tree bbox |

## Key Observations

- **FTS5 is the slowest query** — ~362 µs for 500 routes, ~2.4 ms for 5000 routes. This is the primary metric to watch when comparing drivers.
- **R*Tree bbox is the fastest spatial query** — ~24 µs for 500 routes. Spatial indexing is very efficient.
- **Haversine over full table** is surprisingly fast (~2 µs) because the Go-registered function is lightweight.
- **Bulk insert** scales roughly linearly: 100 routes take ~2.5× longer than 10 routes.

## How to Run

```bash
make test
# or
CGO_ENABLED=1 go test -tags "sqlite,sqlite_fts5" -bench=. -benchmem -count=1 -run=^$ ./repositories/
```

## Future Comparisons

This file will be extended with results from additional driver variants (e.g., `modernc.org/sqlite`) as they become available. Each variant will be added as a new section with its own environment details.
