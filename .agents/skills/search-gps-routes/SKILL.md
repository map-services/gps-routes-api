---
name: search-gps-routes
version: 1.0.0
type: api-tool
description: >-
  Search the GPS Walking Routes REST API using curl and jq exclusively to fetch
  and format route data, including reference-data lookup, faceted search,
  nearby/bounding-box filtering, and single-route metadata retrieval.
category: data-retrieval
api:
  base_url: https://api.hz-nbg1.destructuring-bind.org
  spec_source: openapi-spec/gps-routes.yaml
  auth:
    method: api-key-header
    header_name: X-API-Key
    env_var: GPS_ROUTES_API_KEY
endpoints:
  - path: /v1/gps-routes/ref-data
    method: GET
    purpose: Download reference data (facets, attribution)
  - path: /v1/gps-routes/search
    method: POST
    purpose: Perform a faceted or keyword search for GPS routes
  - path: /v1/gps-routes/{objectID}
    method: GET
    purpose: Fetch a single route's full metadata
tools_required:
  - curl
  - jq
output_format: json
summary: false
---

# Skill: Search GPS Routes (REST API)

Search the GPS Walking Routes API using `curl` and `jq` **exclusively** to fetch
and format data. No other HTTP clients, scripting languages, or JSON parsers
are permitted for querying this API.

## Spec source of truth

Always consult `openapi-spec/gps-routes.yaml` for the authoritative endpoint
definitions, request/response schemas, valid facet fields, and auth requirements.
Do **not** rely on memory or hard-coded field names — the spec describes:

- `POST /v1/gps-routes/search` → `SearchRequest` (body) → `SearchResults`
- `GET  /v1/gps-routes/{objectID}` → `RouteMetadata`
- `GET  /v1/gps-routes/ref-data` → `RefData`

## Authentication

The API is protected by an API key sent in the `X-API-Key` request header. The
key value lives in the project `.env` file under `GPS_ROUTES_API_KEY` and MUST
be passed on every request:

```bash
API_KEY="$(grep '^GPS_ROUTES_API_KEY=' .env | sed 's/^GPS_ROUTES_API_KEY=//')"
```

A missing or invalid key returns `401`. Include `-H "X-API-Key: $API_KEY"` on
every `curl` call. Also include `-H "Content-Type: application/json"` for
`POST` requests.

Base URL: `https://api.hz-nbg1.destructuring-bind.org` (use the live/staging
server that matches the `.env` configuration).

## Always: fetch reference data first

Before performing any targeted search, download the **reference data** so you can
understand the available faceted categories and their valid values:

```bash
curl -sS \
  "https://api.hz-nbg1.destructuring-bind.org/v1/gps-routes/ref-data" \
  -H "X-API-Key: $API_KEY" | jq '.'
```

The response contains:

- `facets` — a map of facet field → `{ value → count }`, e.g.
  `country`, `state`, `region`, `county`, `district`, `activities`,
  `facilities`, `points_of_interest`, `route_type`, `difficulty`, `terrain`,
  `estimated_duration`. **Only these keys are accepted** as `facets` filters in
  a search request; unknown facet fields are rejected with `400`.
- `attribution` — the data-source attribution strings.

Inspect the facet names and popular values, then use them to build **targeted**
search requests.

## Targeted search

Search with `POST /v1/gps-routes/search`. The request body is a `SearchRequest`:

```bash
curl -sS \
  "https://api.hz-nbg1.destructuring-bind.org/v1/gps-routes/search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  --data-raw "$(jq -nc \
    --arg q "coffee shop" \
    --argjson offset 0 \
    --argjson limit 24 \
    '{query:$q, offset:$offset, limit:$limit, truncateText:true}')" \
  | jq '.'
```

Field semantics (per the spec):

- `query` *(required, string)* — full-text search phrase. See Query Syntax below.
- `offset` *(integer, 0..100000, default 0)* — pagination offset.
- `limit` *(integer, 0..100, default 10 when omitted/0)* — page size.
- `boundingBox` *(array of 4 floats: bottom-left lng/lat then top-right lng/lat)* —
  mutually exclusive with `nearby`.
- `facets` *(object: facetField → string[])* — filter by facet values; each key
  must be a valid facet from the reference data.
- `nearby` *(NearbyFilter: place string, optional center, optional distanceKm)* —
  proximity search via the OS Names API; **mutually exclusive** with `boundingBox`.
- `truncateText` *(boolean)* — truncate long descriptions in summary results.
- `skipFacets` *(boolean)* — omit facet counts from the response.

### Query Syntax

The `query` field uses SQLite FTS5 full-text search against three columns:
`title` (highest weight), `display_address` (medium), and `description` (lowest).

**Every query is automatically converted to a prefix search.** Each word gets
a wildcard appended, so `"canal walk"` matches "canal walk", "canal walking",
"canal walker", etc.

**Supported operators:**

| Syntax | Description | Example |
|--------|-------------|---------|
| Plain words | Prefix match, words are ANDed | `"canal walking"` matches "canal*" AND "walking*" |
| `"quoted phrase"` | Exact phrase (no prefix) | `"river walk"` matches the exact phrase |
| `NEAR/n` | Words within n tokens | `"canal" NEAR/5 "bridge"` |
| `-term` | Exclude a term | `"walking -cycling"` |
| `term1 OR term2` | Match either | `"river OR canal"` |

### Bounding box search

To search within a rectangular area, provide `boundingBox` as an array of 4 floats
(bottom-left lng/lat, then top-right lng/lat). Mutually exclusive with `nearby`:

```bash
curl -sS \
  "https://api.hz-nbg1.destructuring-bind.org/v1/gps-routes/search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  --data-raw "$(jq -nc \
    --argjson bbox '[-0.2, 54.0, 0.2, 54.5]' \
    --argjson limit 10 \
    '{query:"", boundingBox:$bbox, limit:$limit, truncateText:true}')" \
  | jq '.hits[] | {ref, title, distance_km, _geoloc}'
```

### Facets filtering

Narrow results by facet values. Each key must be a valid facet field from the
reference data (e.g., `country`, `difficulty`, `route_type`, `activities`):

```bash
curl -sS \
  "https://api.hz-nbg1.destructuring-bind.org/v1/gps-routes/search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  --data-raw "$(jq -nc \
    --arg q "abbey" \
    --argjson facets '{country: ["United Kingdom"], difficulty: ["easy", "moderate"]}' \
    --argjson limit 10 \
    '{query:$q, facets:$facets, limit:$limit, truncateText:true}')" \
  | jq '.hits[] | {ref, title, difficulty, country}'
```

### Nearby search

Search for routes near a place name. The `place` field is resolved via the OS
Names API. Optionally provide `center` (lat/lng) and `distanceKm`:

```bash
curl -sS \
  "https://api.hz-nbg1.destructuring-bind.org/v1/gps-routes/search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  --data-raw "$(jq -nc \
    --argjson nearby '{place: "Helmsley", distanceKm: 20}' \
    --argjson limit 10 \
    '{query:"", nearby:$nearby, limit:$limit, truncateText:true}')" \
  | jq '.hits[] | {ref, title, distance_km, _geoloc}'
```

### Response shape (`SearchResults`)

```bash
... | jq '{total, hits: [.hits[] | {objectID, ref, title}], facets}'
```

- `total` — total matching route count.
- `hits` — array of `RouteSummary` (each has `objectID`, `ref`, `title`,
  `description`, `headline_image_url`, `distance_km`, `_geoloc:{lat,lng}`).
- `facets` — facet value counts.
- `attribution` — data-source attribution strings.

## Fetch a single route

```bash
curl -sS \
  "https://api.hz-nbg1.destructuring-bind.org/v1/gps-routes/${OBJECT_ID}" \
  -H "X-API-Key: $API_KEY" | jq '.'
```

Returns a `RouteMetadata` object extending `RouteSummary` with the following
additional fields:

- `created_at` — ISO 8601 timestamp when the route was generated.
- `nearby` — array of `RouteSummary` objects for nearby walking routes.
- `details` — array of `Detail` objects (each has `subtitle` and `content`).
- `images` — array of `Image` objects (each has `src`, optional `title` and `caption`).
- `gpx_url` — URL pointing to the associated GPX route data.
- `video_url` — link to a YouTube video (nullable).
- `display_address` — human-friendly display address (nullable).
- `postcode` — nearby postcode (nullable).
- `district` — district name (nullable).
- `county` — county name (nullable).
- `region` — region name (nullable).
- `state` — state or country subdivision (nullable).
- `country` — country name (nullable).
- `estimated_duration` — estimated time to complete the route (nullable).
- `difficulty` — difficulty grade (nullable).
- `terrain` — array of terrain type strings (nullable).
- `points_of_interest` — array of point-of-interest strings (nullable).
- `facilities` — array of facility strings (nullable).
- `route_type` — type of route, e.g. "circular" (nullable).
- `activities` — array of activity strings (nullable).

Useful jq projections:

```bash
# Quick overview of a route
curl -sS ... | jq '{ref, title, distance_km, difficulty, estimated_duration, route_type}'

# List all image URLs
curl -sS ... | jq '[.images[].src]'

# List nearby routes with distances
curl -sS ... | jq '.nearby[] | {ref, title, distance_km}'

# Show detail sections
curl -sS ... | jq '.details[] | {subtitle, content}'
```

## Error codes

| Code | Meaning |
|------|--------|
| `400` | Invalid request: bad offset/limit range, both `boundingBox` and `nearby` specified, unknown facet field, or missing required fields |
| `401` | Missing or invalid API key |
| `404` | Route not found for the given `objectID` |
| `500` | Internal server error |

## jq usage conventions

- Use `jq -r '.field'` for raw scalar output.
- Use `jq '. | {a, b}'` to project specific fields.
- Use `jq '.hits[] | {objectID, ref, title, distance_km, _geoloc}'` to list
  compact results.
- Pipe every response through `jq` for readable, structured output.
- Extract image URLs: `jq '[.images[].src]'`
- List nearby routes: `jq '.nearby[] | {ref, title, distance_km}'`
- Show detail sections: `jq '.details[] | {subtitle, content}'`
