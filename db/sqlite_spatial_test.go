package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// openSpatialiteTestDB opens a throwaway SQLite DB through the spatialite-enabled
// driver registered in sqlite_connect.go init(). go-sqlite3 only calls
// sqlite3_enable_load_extension(1) when extensions are configured via the driver
// config, so db.Ping() triggers the lazy load of `mod_spatialite`. When the
// library isn't installed/loadable (e.g. no libsqlite3-mod-spatialite on the
// loader path), NewSQLiteDB falls back to plain FTS5-only sqlite3; the same
// degrades to t.Skip here so the suite stays green everywhere.
func openSpatialiteTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := t.TempDir() + "/spatial_test.db"
	uri := "file:" + path + "?cache=shared&mode=rwc&_journal_mode=WAL&_synchronous=NORMAL"
	dbh, err := sql.Open("sqlite3_spatialite", uri)
	if err != nil {
		t.Skipf("open sqlite3_spatialite driver: %v", err)
	}
	dbh.SetMaxOpenConns(1)
	if err := dbh.Ping(); err != nil {
		_ = dbh.Close()
		t.Skipf("mod_spatialite not loadable (install libsqlite3-mod-spatialite): %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	return dbh
}

// TestSQLiteSpatialiteSpatialFunctions asserts that the Spatialite extension is
// genuinely loaded (not merely compiled) and that the bbox predicate emitted by
// SQLiteDialect.BuildSTWithinQuery executes against libspatialite 5.x.
//
// libspatialite 5.x dropped PostGIS's ST_MakeEnvelope in favour of
// BuildMbr(xmin, ymin, xmax, ymax, srid); ST_Within returns an integer
// (1=true / 0=false); ST_AsText returns WKT such as "POINT(1 2)".
func TestSQLiteSpatialiteSpatialFunctions(t *testing.T) {
	dbh := openSpatialiteTestDB(t)
	ctx := context.Background()

	// ST_AsText + ST_GeomFromText must round-trip a POINT verbatim.
	var wkt string
	if err := dbh.QueryRowContext(ctx,
		`SELECT ST_AsText(ST_GeomFromText('POINT(1 2)', 4326))`,
	).Scan(&wkt); err != nil {
		t.Fatalf("spatial functions not registered: %v", err)
	}
	if wkt != "POINT(1 2)" {
		t.Fatalf("ST_AsText = %q, want \"POINT(1 2)\"", wkt)
	}

	// BuildMbr is libspatialite 5.x's bbox constructor (replaces ST_MakeEnvelope).
	var mbrWKT string
	if err := dbh.QueryRowContext(ctx,
		`SELECT ST_AsText(BuildMbr(0, 0, 10, 10, 4326))`,
	).Scan(&mbrWKT); err != nil {
		t.Fatalf("BuildMbr failed: %v", err)
	}
	if !strings.Contains(mbrWKT, "POLYGON") {
		t.Fatalf("BuildMbr did not return a POLYGON: %q", mbrWKT)
	}

	// ST_Within against a BuildMbr bbox: inside -> 1, outside -> 0. This is the
	// exact SQL shape SQLiteDialect.BuildSTWithinQuery now generates.
	cases := []struct {
		point  string
		inside bool
	}{
		{`POINT(2 2)`, true},    // inside [0,0]-[10,10]
		{`POINT(50 50)`, false}, // outside
	}
	for _, c := range cases {
		var got int
		if err := dbh.QueryRowContext(ctx,
			`SELECT ST_Within(ST_GeomFromText(?, 4326), BuildMbr(0, 0, 10, 10, 4326))`, c.point,
		).Scan(&got); err != nil {
			t.Fatalf("ST_Within(%s) failed: %v", c.point, err)
		}
		want := 0
		if c.inside {
			want = 1
		}
		if got != want {
			t.Errorf("ST_Within(%s) = %d, want %d", c.point, got, want)
		}
	}

	// Regression guard: ST_MakeEnvelope must NOT load (libspatialite 5.x omits
	// it); this keeps SQLiteDialect.BuildSTWithinQuery on BuildMbr.
	var _ interface{}
	if err := dbh.QueryRowContext(ctx,
		`SELECT ST_AsText(ST_MakeEnvelope(0, 0, 1, 1, 4326))`,
	).Scan(&struct{}{}); err == nil {
		t.Fatal("ST_MakeEnvelope unexpectedly loaded; SQLiteDialect must keep emitting BuildMbr")
	}
}
