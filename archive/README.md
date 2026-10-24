# GPS Routes Archive — Legacy Python Scripts

This directory contains legacy Python scripts that were used to crawl, enrich, and export route data from `gps-routes.co.uk`. The current Go API reads from the pre-processed `data/backup/` JSON files produced by these scripts.

## Setup

These scripts use [uv](https://docs.astral.sh/uv/) for dependency management.

### Prerequisites

- Python 3.11 or later
- [uv](https://docs.astral.sh/uv/getting-started/installation/) (`curl -LsSf https://astral.sh/uv/install.sh | sh`)

### Install dependencies

```bash
cd archive
uv sync
```

This creates a virtual environment and installs all dependencies from `pyproject.toml`.

### Configure environment

Copy `.env.example` to `.env` and fill in your API keys:

```bash
cp .env.example .env
```

Required variables depend on which scripts you run:

| Variable | Used by | Purpose |
|---|---|---|
| `ALGOLIA_APP_ID` | `download.py`, `upload.py` | Algolia index access |
| `ALGOLIA_API_KEY` | `download.py`, `upload.py` | Algolia index access |
| `OS_DATAHUB_API_KEY` | `osdatahub_names.py` | UK Ordnance Survey gazetteer |
| `MONGO_URI` | `mongo_import.py` | MongoDB connection string |

### Run a script

```bash
uv run crawler.py
uv run nominatim.py
uv run full_list.py
```

## Pipeline Overview

The typical data collection workflow was:

1. **`full_list.py`** — Paginates through the entire `RoutesLinksWalks` view on gps-routes.co.uk to discover all route URLs. Saves the list to `data/full-list.txt`.

2. **`crawler.py`** — Main entry point with two crawl modes:
   - `random_page_crawl` — Picks a random index page, extracts all route links, then crawls each detail page via `DetailExtractor`.
   - `unprocessed_entries_crawl` — Reads `data/missing-geoloc.txt` (routes missing geolocation data), re-crawls the detail page, and stores the record only if it now has geolocation data.

3. **`detail_extractor.py`** — Parses an HTML detail page and extracts metadata (title, ref, headline image), nearby routes, GPX download link, photos, distance, description, and video link. Downloads and parses the GPX file to extract the first point's coordinates as `_geoloc`.

4. **`unprocessed_*` scripts** — Gap-finding utilities:
   - `unprocessed_list.py` — Compares `full-list.txt` against crawled records to find unprocessed routes → writes `unprocessed-list.txt`.
   - `unprocessed_geoloc.py` — Finds records lacking `_geoloc` → writes `missing-geoloc.txt`.
   - `unprocessed_country.py` — Finds records with geolocation but missing `country` → writes `missing-country.txt`.
   - `unprocessed_gpx.py` — For records missing `_geoloc`, hits a mobile map endpoint to extract GPX data and save the geolocation.

5. **`nominatim.py`** — Reverse geocodes each route's `_geoloc` via OpenStreetMap's Nominatim API to populate `postcode`, `district`, `county`, `region`, `state`, `country`. Runs at 1 request/second.

6. **`osdatahub_names.py`** — Wrapper around the UK Ordnance Survey DataHub Names API. Converts lat/lon to British National Grid (EPSG:27700), then queries the nearest gazetteer entry for location data.

7. **`fetch_routes.py`** — Iterates over all JSON records in `data/backup/`, downloads any missing GPX files, and saves them to `data/routes/{first-char}/{object-id}.gpx`.

8. **`update_gpx_url.py`** — Rewrites `gpx_url` fields to point at the GitHub raw URL instead of the original site.

9. **`download.py`** / **`upload.py`** — Browse and save records from an Algolia index, or batch-upload local JSON files to Algolia.

10. **`mongo_import.py`** — Imports JSON records into MongoDB, converting `_geoloc` to GeoJSON `Point` format and creating text/2dsphere/facet indexes.

## Core Script Summary

| Script | Role |
|---|---|
| `crawler.py` | Main orchestrator — scrape detail pages, extract GPX, save JSON + GPX files |
| `detail_extractor.py` | Parse HTML pages, download GPX, extract coordinates |
| `full_list.py` | Discover all route URLs via pagination |
| `nominatim.py` | Reverse geocode via OpenStreetMap Nominatim API |
| `osdatahub_names.py` | UK OS DataHub Names gazetteer lookup |
| `fetch_routes.py` | Download missing GPX files |
| `download.py` | Pull records from Algolia index |
| `upload.py` | Push records to Algolia index |
| `mongo_import.py` | Import records into MongoDB |
| `unprocessed_list.py` | Find routes not yet crawled |
| `unprocessed_geoloc.py` | Find records missing geolocation |
| `unprocessed_gpx.py` | Extract geolocation from mobile map endpoint |
| `unprocessed_country.py` | Find records missing country field |
| `update_gpx_url.py` | Rewrite GPX URLs to GitHub raw links |
| `utils.py` | Shared helpers — object ID generation, Algolia client, OS DataHub client |
| `llama.py` | LLM-based enrichment script |

## Dependencies

| Package | Purpose |
|---|---|
| `algoliasearch` | Algolia search client |
| `beautifulsoup4` | HTML parsing |
| `gpxpy` | GPX file parsing |
| `lxml` | XML/HTML parser backend for BeautifulSoup |
| `pymongo` | MongoDB driver |
| `python-dotenv` | Load `.env` environment variables |
| `pyproj` | Coordinate projection (lat/lon → British National Grid) |
| `requests` | HTTP client |
| `tqdm` | Progress bars |
