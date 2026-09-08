-- IPGrab schema. Applied idempotently on startup.

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
