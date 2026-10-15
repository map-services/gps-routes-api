-- SQLite3 Schema for gps-routes-api
-- Created: 12 April 2026

-- Enable required features
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

-- Load extensions (Spatialite optional; FTS5 needed for full-text search)
-- SELECT load_extension('mod_spatialite');
-- SELECT load_extension('fts5');

-- Main routes table
-- Changes from the original schema:
-- - TEXT[] → JSON (terrain, activities, facilities, points_of_interest)
-- - GEOMETRY(Point, 4326) → BLOB (WKB geometry with Spatialite)
-- - TSVECTOR → Separate FTS5 virtual table (routes_fts)
CREATE TABLE routes (
    object_id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ref TEXT NOT NULL,
    title TEXT NOT NULL,
    headline_image_url TEXT,
    gpx_url TEXT,
    latitude REAL,
    longitude REAL,
    _geoloc BLOB,  -- WKB geometry point (Spatialite compatible)
    distance_km REAL,
    description TEXT NOT NULL,
    video_url TEXT,
    display_address TEXT,
    postcode TEXT,
    district TEXT,
    county TEXT,
    region TEXT,
    state TEXT,
    country TEXT,
    estimated_duration TEXT,
    difficulty TEXT,
    terrain JSON,  -- Array: ["rock", "grass", ...]
    points_of_interest JSON,  -- Array: ["waterfall", "viewpoint", ...]
    facilities JSON,  -- Array: ["parking", "cafe", ...]
    route_type TEXT,
    activities JSON,  -- Array: ["hiking", "walking", ...]
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Scalar field indexes (B-tree)
CREATE INDEX idx_routes_district ON routes (district);
CREATE INDEX idx_routes_county ON routes (county);
CREATE INDEX idx_routes_region ON routes (region);
CREATE INDEX idx_routes_state ON routes (state);
CREATE INDEX idx_routes_country ON routes (country);
CREATE INDEX idx_routes_route_type ON routes (route_type);
CREATE INDEX idx_routes_estimated_duration ON routes (estimated_duration);
CREATE INDEX idx_routes_difficulty ON routes (difficulty);

-- Expression indexes for JSON array queries
-- These optimize queries like: WHERE json_extract(activities, '$[*]') LIKE '%hiking%'
CREATE INDEX idx_routes_activities_expr ON routes (json_extract(activities, '$'));
CREATE INDEX idx_routes_terrain_expr ON routes (json_extract(terrain, '$'));
CREATE INDEX idx_routes_facilities_expr ON routes (json_extract(facilities, '$'));
CREATE INDEX idx_routes_poi_expr ON routes (json_extract(points_of_interest, '$'));

-- Spatial index (if using Spatialite)
-- CREATE INDEX idx_routes_geolocation ON routes (_geoloc) WHERE _geoloc IS NOT NULL;
-- Alternatively without Spatialite, create bbox indexes on lat/lng
CREATE INDEX idx_routes_latitude ON routes (latitude) WHERE latitude IS NOT NULL;
CREATE INDEX idx_routes_longitude ON routes (longitude) WHERE longitude IS NOT NULL;

-- Full-text search virtual table (FTS5)
-- Maps to: the original TSVECTOR search vector with weighted columns
CREATE VIRTUAL TABLE routes_fts USING fts5(
    title,           -- Weight A (highest relevance)
    display_address, -- Weight B (medium)
    description,     -- Weight C (lowest)
    content=routes,
    content_rowid=rowid
);

-- Triggers to keep FTS5 table in sync with routes table
CREATE TRIGGER routes_fts_insert AFTER INSERT ON routes BEGIN
    INSERT INTO routes_fts (rowid, title, display_address, description)
    VALUES (NEW.rowid, NEW.title, NEW.display_address, NEW.description);
END;

CREATE TRIGGER routes_fts_update AFTER UPDATE ON routes BEGIN
    UPDATE routes_fts
    SET title = NEW.title, display_address = NEW.display_address, description = NEW.description
    WHERE rowid = NEW.rowid;
END;

CREATE TRIGGER routes_fts_delete AFTER DELETE ON routes BEGIN
    DELETE FROM routes_fts WHERE rowid = OLD.rowid;
END;

-- Related tables (same structure, but adapted)
CREATE TABLE nearby (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    route_object_id TEXT NOT NULL,
    description TEXT,
    object_id TEXT,
    ref TEXT,
    FOREIGN KEY (route_object_id) REFERENCES routes(object_id) ON DELETE CASCADE
);

CREATE INDEX idx_nearby_route_object_id ON nearby (route_object_id);

CREATE TABLE images (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    route_object_id TEXT NOT NULL,
    src TEXT NOT NULL,
    title TEXT,
    caption TEXT,
    FOREIGN KEY (route_object_id) REFERENCES routes(object_id) ON DELETE CASCADE
);

CREATE INDEX idx_images_route_object_id ON images (route_object_id);

CREATE TABLE details (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    route_object_id TEXT NOT NULL,
    subtitle TEXT NOT NULL,
    content TEXT NOT NULL,
    FOREIGN KEY (route_object_id) REFERENCES routes(object_id) ON DELETE CASCADE
);

CREATE INDEX idx_details_route_object_id ON details (route_object_id);

-- Pragmas for performance (run after schema creation)
-- PRAGMA optimize;
-- PRAGMA analyze;
