package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/spignelon/netra/internal/models"
)

// rawOpen opens a sqlite handle the same way Open does, but without running
// any migrations — used to simulate database states Open itself would never
// produce (a brand-new empty file, or a pre-migration-framework schema).
func rawOpen(t *testing.T, path string) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("rawOpen: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("rawOpen ping: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

func tableNames(t *testing.T, sqlDB *sql.DB) map[string]bool {
	t.Helper()
	rows, err := sqlDB.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		names[name] = true
	}
	return names
}

// --- Scenario 1: brand-new install ---

func TestOpen_FreshDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netra.db")

	database, err := Open(path)
	if err != nil {
		t.Fatalf("Open on fresh path: %v", err)
	}
	defer database.Close()

	names := tableNames(t, database.DB)
	for _, want := range []string{"admin", "sessions", "links", "events", "settings", "goose_db_version"} {
		if !names[want] {
			t.Errorf("expected table %q to exist after a fresh Open, got tables: %v", want, names)
		}
	}

	exists, err := database.AdminExists()
	if err != nil {
		t.Fatalf("AdminExists: %v", err)
	}
	if exists {
		t.Errorf("AdminExists on a fresh DB = true, want false")
	}
	if err := database.CreateAdmin("admin", "hash"); err != nil {
		t.Fatalf("CreateAdmin on freshly migrated schema: %v", err)
	}

	version, err := getGooseVersion(database.DB)
	if err != nil {
		t.Fatalf("getGooseVersion: %v", err)
	}
	if version != 1 {
		t.Errorf("goose version after fresh Open = %d, want 1", version)
	}
}

// --- Scenario 2: repeated startups against the same file must be a no-op ---

func TestOpen_IdempotentAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netra.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := first.CreateAdmin("admin", "hash"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first handle: %v", err)
	}

	// Simulate the binary restarting three times in a row (e.g. a crash-loop,
	// or just three normal `docker compose up`s) against the same data.
	for i := 0; i < 3; i++ {
		reopened, err := Open(path)
		if err != nil {
			t.Fatalf("restart %d: Open: %v", i, err)
		}
		exists, err := reopened.AdminExists()
		if err != nil {
			t.Fatalf("restart %d: AdminExists: %v", i, err)
		}
		if !exists {
			t.Fatalf("restart %d: admin row lost across restart", i)
		}
		var rowCount int
		if err := reopened.QueryRow(`SELECT COUNT(*) FROM goose_db_version WHERE version_id = 1 AND is_applied = 1`).Scan(&rowCount); err != nil {
			t.Fatalf("restart %d: count goose_db_version rows: %v", i, err)
		}
		if rowCount != 1 {
			t.Errorf("restart %d: migration 1 recorded as applied %d times, want exactly 1", i, rowCount)
		}
		if err := reopened.Close(); err != nil {
			t.Fatalf("restart %d: close: %v", i, err)
		}
	}
}

// --- Scenario 3: an existing user upgrading from a pre-migration-framework
// build of Netra, whose database was created by a plain, un-versioned
// `CREATE TABLE IF NOT EXISTS` schema apply (exactly what internal/db/db.go
// did before this change) and already has real data in it. ---

const legacyPreMigrationSchema = `
CREATE TABLE IF NOT EXISTS admin (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL,
    password_hash TEXT    NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT    PRIMARY KEY,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS links (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slug       TEXT    NOT NULL UNIQUE,
    type       TEXT    NOT NULL,
    label      TEXT    NOT NULL DEFAULT '',
    config     TEXT    NOT NULL DEFAULT '{}',
    active     INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_links_slug ON links(slug);
CREATE TABLE IF NOT EXISTS events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    link_id         INTEGER NOT NULL,
    type            TEXT    NOT NULL,
    ts              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ip              TEXT    NOT NULL DEFAULT '',
    country         TEXT    NOT NULL DEFAULT '',
    region          TEXT    NOT NULL DEFAULT '',
    city            TEXT    NOT NULL DEFAULT '',
    lat             REAL    NOT NULL DEFAULT 0,
    lon             REAL    NOT NULL DEFAULT 0,
    isp             TEXT    NOT NULL DEFAULT '',
    org             TEXT    NOT NULL DEFAULT '',
    asn             TEXT    NOT NULL DEFAULT '',
    user_agent      TEXT    NOT NULL DEFAULT '',
    device          TEXT    NOT NULL DEFAULT '',
    os              TEXT    NOT NULL DEFAULT '',
    browser         TEXT    NOT NULL DEFAULT '',
    referer         TEXT    NOT NULL DEFAULT '',
    accept_language TEXT    NOT NULL DEFAULT '',
    headers_json    TEXT    NOT NULL DEFAULT '{}',
    gps_lat         REAL,
    gps_lon         REAL,
    gps_accuracy    REAL,
    FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_events_link ON events(link_id);
CREATE INDEX IF NOT EXISTS idx_events_ts   ON events(ts);
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

func TestOpen_UpgradesExistingPreMigrationDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netra.db")

	// Stand up a database exactly the way the old (pre-goose) code did, with
	// real data in it — this is what every existing self-hosted deployment
	// looks like right now.
	legacy := rawOpen(t, path)
	if _, err := legacy.Exec(legacyPreMigrationSchema); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	if _, err := legacy.Exec(`INSERT INTO admin (username, password_hash) VALUES (?, ?)`, "admin", "existing-hash"); err != nil {
		t.Fatalf("seed legacy admin: %v", err)
	}
	res, err := legacy.Exec(`INSERT INTO links (slug, type, label, config, active) VALUES (?, ?, ?, ?, ?)`,
		"my-old-link", "redirect", "Old Link", `{"destination":"https://example.com"}`, 1)
	if err != nil {
		t.Fatalf("seed legacy link: %v", err)
	}
	linkID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("legacy link id: %v", err)
	}
	if _, err := legacy.Exec(`INSERT INTO events (link_id, type, ip, country) VALUES (?, ?, ?, ?)`,
		linkID, "click", "203.0.113.5", "Wonderland"); err != nil {
		t.Fatalf("seed legacy event: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy handle: %v", err)
	}

	// Now run today's code against that exact file — this is the upgrade.
	upgraded, err := Open(path)
	if err != nil {
		t.Fatalf("Open on legacy pre-migration database: %v", err)
	}
	defer upgraded.Close()

	// The old data must survive untouched.
	admin, err := upgraded.GetAdmin()
	if err != nil {
		t.Fatalf("GetAdmin after upgrade: %v", err)
	}
	if admin.Username != "admin" || admin.PasswordHash != "existing-hash" {
		t.Errorf("admin row corrupted by upgrade: got %+v", admin)
	}

	var linkCount, eventCount int
	if err := upgraded.QueryRow(`SELECT COUNT(*) FROM links WHERE slug = 'my-old-link'`).Scan(&linkCount); err != nil {
		t.Fatalf("count legacy link: %v", err)
	}
	if linkCount != 1 {
		t.Errorf("legacy link lost during upgrade, count = %d", linkCount)
	}
	if err := upgraded.QueryRow(`SELECT COUNT(*) FROM events WHERE ip = '203.0.113.5'`).Scan(&eventCount); err != nil {
		t.Fatalf("count legacy event: %v", err)
	}
	if eventCount != 1 {
		t.Errorf("legacy event lost during upgrade, count = %d", eventCount)
	}

	// And the migration framework must now consider itself caught up, on the
	// very first upgrade, with no manual intervention.
	version, err := getGooseVersion(upgraded.DB)
	if err != nil {
		t.Fatalf("getGooseVersion after upgrade: %v", err)
	}
	if version != 1 {
		t.Errorf("goose version after upgrading a legacy database = %d, want 1", version)
	}

	// The app must remain fully functional post-upgrade — exercise a normal
	// write path, not just reads.
	newLink := &models.Link{Slug: "post-upgrade-link", Type: models.TypeRedirect, Active: true}
	if _, err := upgraded.CreateLink(newLink); err != nil {
		t.Fatalf("CreateLink after upgrade: %v", err)
	}
}

// --- Scenario 4: a future schema change (a new migration added after this
// one ships) applies correctly both to a fresh database and to one that's
// already at the previous version — proving the ongoing "new changes also
// auto-migrate" behavior the app relies on, independent of the real
// production migrations directory. ---

func TestMigrate_FutureMigrationsApplyGoingForward(t *testing.T) {
	baseline := fstest.MapFS{
		"00001_create_widgets.sql": &fstest.MapFile{Data: []byte(`
-- +goose Up
CREATE TABLE widgets (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL
);
-- +goose Down
DROP TABLE widgets;
`)},
	}
	withNewColumn := fstest.MapFS{
		"00001_create_widgets.sql": baseline["00001_create_widgets.sql"],
		"00002_add_widget_color.sql": &fstest.MapFile{Data: []byte(`
-- +goose Up
ALTER TABLE widgets ADD COLUMN color TEXT NOT NULL DEFAULT 'gray';
-- +goose Down
-- sqlite ALTER TABLE cannot drop columns without a table rebuild; not
-- exercised by the app, so left unimplemented for this test fixture.
`)},
	}

	path := filepath.Join(t.TempDir(), "widgets.db")
	sqlDB := rawOpen(t, path)

	// "Yesterday's" release: only migration 1 exists.
	if err := migrate(sqlDB, baseline); err != nil {
		t.Fatalf("apply baseline migration: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO widgets (name) VALUES ('pre-existing widget')`); err != nil {
		t.Fatalf("seed widget before the new migration exists: %v", err)
	}

	// "Today's" release ships migration 2 alongside it, exactly like adding a
	// new .sql file to internal/db/migrations/ would.
	if err := migrate(sqlDB, withNewColumn); err != nil {
		t.Fatalf("apply forward migration: %v", err)
	}

	var name, color string
	if err := sqlDB.QueryRow(`SELECT name, color FROM widgets WHERE name = 'pre-existing widget'`).Scan(&name, &color); err != nil {
		t.Fatalf("query pre-existing row after column was added: %v", err)
	}
	if color != "gray" {
		t.Errorf("pre-existing row's new column = %q, want default %q", color, "gray")
	}

	version, err := getGooseVersion(sqlDB)
	if err != nil {
		t.Fatalf("getGooseVersion: %v", err)
	}
	if version != 2 {
		t.Errorf("version after forward migration = %d, want 2", version)
	}

	// Running migrate() again (next restart) must not re-run or fail.
	if err := migrate(sqlDB, withNewColumn); err != nil {
		t.Fatalf("re-running migrate() on an up-to-date db: %v", err)
	}
	var appliedTwice int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM goose_db_version WHERE version_id = 2`).Scan(&appliedTwice); err != nil {
		t.Fatalf("count version 2 rows: %v", err)
	}
	if appliedTwice != 1 {
		t.Errorf("migration 2 applied-row count = %d after a repeat run, want 1", appliedTwice)
	}
}

// getGooseVersion reads the current schema version directly out of goose's
// own tracking table, sidestepping goose's Provider (which refuses to report
// a version against a filesystem containing no migrations at all).
func getGooseVersion(sqlDB *sql.DB) (int64, error) {
	var version sql.NullInt64
	err := sqlDB.QueryRow(`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1`).Scan(&version)
	if err != nil {
		return 0, err
	}
	return version.Int64, nil
}
