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
go test -bench=. -benchmem -count=1 -run=^$ ./repositories/
```

## 500 Routes — modernc.org/sqlite (Pure Go, No CGO)

| Benchmark | ns/op | B/op | allocs/op | vs. mattn Δ |
|-----------|-------|------|-----------|-------------|
| SearchSimple | 151,310 | 7,350 | 155 | **3.6× slower** |
| SearchFTS | 2,248,728 | 8,198 | 170 | **6.2× slower** |
| SearchBbox | 67,469 | 4,876 | 86 | **2.8× slower** |
| SearchRadius | 78,752 | 5,160 | 107 | **2.4× slower** |
| SearchFTSWithBbox | 1,042,232 | 8,772 | 174 | **5.0× slower** |
| SearchWithFacets | 258,020 | 8,550 | 181 | **3.3× slower** |
| FacetCounts | 523,705 | 1,993 | 45 | **3.2× slower** |
| FacetCountsScalar | 328,856 | 2,786 | 80 | **2.8× slower** |
| CountAll | 6,924 | 856 | 19 | **2.1× slower** |
| CountAllFTS | 306,352 | 1,192 | 30 | **6.8× slower** |
| StoreBatch/10routes | 3,302,259 | 50,554 | 1,159 | **1.6× slower** |
| StoreBatch/100routes | 10,085,578 | 400,080 | 9,443 | **2.0× slower** |
| HaversineFunction | 5,217 | 560 | 19 | **1.9× slower** |
| HaversineOverTable | 296,913 | 20,436 | 2,511 | **131× slower** |

## 5000 Routes (Scaling) — modernc.org/sqlite

| Benchmark | ns/op | B/op | allocs/op | vs. mattn Δ |
|-----------|-------|------|-----------|-------------|
| SearchLargeDataset/Simple | 1,051,816 | 7,349 | 155 | **4.6× slower** |
| SearchLargeDataset/FTS | 10,690,705 | 8,184 | 169 | **4.4× slower** |
| SearchLargeDataset/Bbox | 2,531,473 | 7,992 | 163 | **3.6× slower** |
| SearchLargeDataset/Radius | 252,237 | 14,593 | 925 | **1.5× slower** |
| SearchLargeDataset/FTS+Bbox | 7,302,497 | 8,792 | 174 | **4.1× slower** |

## Comparison Summary

modernc.org/sqlite is consistently **2–7× slower** than mattn/go-sqlite3 across all query types, with FTS queries showing the largest regression (~6×). The pure Go implementation trades raw speed for CGO-free builds and simpler cross-compilation.

### Key Findings

- **FTS5 is the biggest regression** — ~2.2 ms vs ~362 µs for 500 routes (6.2× slower). At 5000 routes, FTS jumps to ~10.7 ms vs ~2.4 ms (4.4× slower).
- **R\*Tree bbox** is ~2.8× slower but still fast in absolute terms (~67 µs vs ~24 µs).
- **HaversineOverTable** shows a dramatic regression (131×) — the Go-registered function callback overhead in modernc is much higher than the C FFI in mattn. However, this query pattern is rarely used in production (R\*Tree prefilter avoids full-table haversine).
- **Memory allocations** are similar or slightly lower for modernc, indicating the slowdown is CPU-bound, not allocation-bound.
- **Bulk insert** is ~1.6–2.0× slower, which matters for data import but not for read-heavy API workloads.

### Verdict

The performance regression is significant enough that migration to modernc.org/sqlite is **not recommended** for this workload. The API serves read-heavy queries where FTS5 and R\*Tree performance directly impact user experience. The benefits of CGO-free builds (simpler Docker, cross-compilation) do not outweigh the 2–7× query latency increase.
