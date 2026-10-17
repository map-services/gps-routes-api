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

> ⚠️ **Two runs were performed:** the first run was on battery power (M2 Pro throttled),
> the second with the laptop charged. The battery run showed ~30% worse times across
> the board, confirming significant CPU throttling. Only the charged run is shown here.

| Benchmark | ns/op (charged) | B/op | allocs/op | vs. mattn Δ |
|-----------|-----------------|------|-----------|-------------|
| SearchSimple | 102,966 | 7,349 | 155 | **2.4× slower** |
| SearchFTS | 1,552,946 | 8,198 | 170 | **4.3× slower** |
| SearchBbox | 45,589 | 4,876 | 86 | **1.9× slower** |
| SearchRadius | 54,231 | 5,161 | 107 | **1.7× slower** |
| SearchFTSWithBbox | 713,994 | 8,769 | 174 | **3.4× slower** |
| SearchWithFacets | 176,463 | 8,549 | 181 | **2.2× slower** |
| FacetCounts | 357,383 | 1,993 | 45 | **2.2× slower** |
| FacetCountsScalar | 235,220 | 2,786 | 80 | **2.0× slower** |
| CountAll | 4,775 | 856 | 19 | **1.4× slower** |
| CountAllFTS | 210,196 | 1,192 | 30 | **4.6× slower** |
| StoreBatch/10routes | 2,674,884 | 50,557 | 1,159 | **1.3× slower** |
| StoreBatch/100routes | 7,085,506 | 399,929 | 9,442 | **1.4× slower** |
| HaversineFunction | 3,578 | 560 | 19 | **1.3× slower** |
| HaversineOverTable | 203,149 | 20,436 | 2,511 | **89× slower** |

## 5000 Routes (Scaling) — modernc.org/sqlite (charged)

| Benchmark | ns/op | B/op | allocs/op | vs. mattn Δ |
|-----------|-------|------|-----------|-------------|
| SearchLargeDataset/Simple | 711,849 | 7,348 | 155 | **3.1× slower** |
| SearchLargeDataset/FTS | 7,384,537 | 8,178 | 169 | **3.0× slower** |
| SearchLargeDataset/Bbox | 1,674,484 | 7,952 | 163 | **2.4× slower** |
| SearchLargeDataset/Radius | 176,514 | 14,431 | 910 | **1.0× slower** |
| SearchLargeDataset/FTS+Bbox | 4,895,615 | 8,784 | 174 | **2.8× slower** |

## Battery vs Charged Impact

The battery-throttled run showed a remarkably consistent ~30% slowdown across all benchmarks:

| Benchmark | Battery ns/op | Charged ns/op | Improvement |
|-----------|--------------|---------------|-------------|
| SearchSimple | 151,310 | 102,966 | 32% faster |
| SearchFTS | 2,248,728 | 1,552,946 | 31% faster |
| SearchBbox | 67,469 | 45,589 | 31% faster |
| HaversineOverTable | 296,913 | 203,149 | 32% faster |

This confirms the M2 Pro applies significant CPU throttling on battery power, affecting
all CPU-bound workloads uniformly.

## Comparison Summary

modernc.org/sqlite is consistently **1.3–4.6× slower** than mattn/go-sqlite3 across most
query types, with FTS queries showing the largest regression (~4×). The pure Go
implementation trades raw speed for CGO-free builds and simpler cross-compilation.

### Key Findings

- **FTS5 remains the biggest regression** — ~1.5 ms vs ~362 µs for 500 routes (4.3× slower). At 5000 routes, FTS is ~7.4 ms vs ~2.4 ms (3.0× slower).
- **R\*Tree bbox** is ~1.9× slower but still fast in absolute terms (~46 µs vs ~24 µs).
- **HaversineOverTable** shows a dramatic regression (89×) — the Go-registered function callback overhead in modernc is much higher than the C FFI in mattn. However, this query pattern is rarely used in production (R\*Tree prefilter avoids full-table haversine).
- **SearchLargeDataset/Radius** is essentially parity (1.0×) — the R*Tree prefilter makes the haversine callback count negligible.
- **Memory allocations** are similar or slightly lower for modernc, indicating the slowdown is CPU-bound, not allocation-bound.
- **Bulk insert** is ~1.3–1.4× slower, which is a minor impact for data import.
- **Battery power** adds ~30% overhead uniformly — always benchmark on charged hardware.

### Verdict

The performance regression is **real but moderate** for most queries (1.3–4.6×). FTS5
queries are the most affected at ~4× slower, but absolute latencies remain in the
sub-millisecond to low-millisecond range. The 89× HaversineOverTable regression is
only relevant for full-table distance sorts, which are rare in production.

Whether to migrate depends on the trade-off between **CGO-free builds** (simpler Docker,
cross-compilation, no `libsqlite3-dev` dependency) and **2–4× query latency increase**.
For a read-heavy API serving interactive users, the latency increase may be acceptable
if absolute response times remain under the user-perceptible threshold (~100 ms).
