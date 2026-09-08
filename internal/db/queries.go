package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/spignelon/ipgrab/internal/models"
)

// sqliteTimeLayouts are the timestamp formats sqlite's CURRENT_TIMESTAMP and
// this driver may hand back as raw text.
var sqliteTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05",
	time.RFC3339,
	time.RFC3339Nano,
}

// parseSQLiteTime parses a raw sqlite timestamp string, trying the layouts
// sqlite is known to produce.
func parseSQLiteTime(s string) (time.Time, error) {
	var lastErr error
	for _, layout := range sqliteTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

// ---- Admin ----

// AdminExists reports whether the instance has been claimed.
func (db *DB) AdminExists() (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM admin`).Scan(&n)
	return n > 0, err
}

// CreateAdmin inserts the single owner account.
func (db *DB) CreateAdmin(username, passwordHash string) error {
	_, err := db.Exec(`INSERT INTO admin (username, password_hash) VALUES (?, ?)`, username, passwordHash)
	return err
}

// GetAdmin returns the owner account, or ErrNotFound if unclaimed.
func (db *DB) GetAdmin() (*models.Admin, error) {
	a := &models.Admin{}
	err := db.QueryRow(`SELECT id, username, password_hash, created_at FROM admin ORDER BY id LIMIT 1`).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// ---- Settings ----

// settingConcealEnabled is the key under which conceal-mode's on/off state
// is stored in the settings table.
const settingConcealEnabled = "conceal_enabled"

// GetSetting returns a raw setting value, or ok=false if it has never been set.
func (db *DB) GetSetting(key string) (value string, ok bool, err error) {
	err = db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// SetSetting upserts a raw setting value.
func (db *DB) SetSetting(key, value string) error {
	_, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ConcealEnabled reports whether conceal mode is currently on. Defaults to
// false (disabled) until explicitly toggled on from the admin settings page.
func (db *DB) ConcealEnabled() (bool, error) {
	v, ok, err := db.GetSetting(settingConcealEnabled)
	if err != nil || !ok {
		return false, err
	}
	return v == "1", nil
}

// SetConcealEnabled persists conceal mode's on/off state.
func (db *DB) SetConcealEnabled(enabled bool) error {
	v := "0"
	if enabled {
		v = "1"
	}
	return db.SetSetting(settingConcealEnabled, v)
}

// ---- Sessions ----

// CreateSession stores a login session token.
func (db *DB) CreateSession(token string, ttl time.Duration) error {
	_, err := db.Exec(`INSERT INTO sessions (token, expires_at) VALUES (?, ?)`,
		token, time.Now().Add(ttl))
	return err
}

// SessionValid reports whether token exists and has not expired.
func (db *DB) SessionValid(token string) (bool, error) {
	var expires time.Time
	err := db.QueryRow(`SELECT expires_at FROM sessions WHERE token = ?`, token).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return time.Now().Before(expires), nil
}

// DeleteSession removes a session (logout).
func (db *DB) DeleteSession(token string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// PurgeExpiredSessions removes stale sessions.
func (db *DB) PurgeExpiredSessions() error {
	_, err := db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now())
	return err
}

// ---- Links ----

// CreateLink inserts a new capture surface and returns its id.
func (db *DB) CreateLink(l *models.Link) (int64, error) {
	cfg, err := json.Marshal(l.Config)
	if err != nil {
		return 0, err
	}
	res, err := db.Exec(`INSERT INTO links (slug, type, label, config, active) VALUES (?, ?, ?, ?, ?)`,
		l.Slug, l.Type, l.Label, string(cfg), boolToInt(l.Active))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SlugExists reports whether a slug is already taken.
func (db *DB) SlugExists(slug string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM links WHERE slug = ?`, slug).Scan(&n)
	return n > 0, err
}

// GetLinkBySlug returns an active-or-not link by its slug.
func (db *DB) GetLinkBySlug(slug string) (*models.Link, error) {
	return db.scanLink(db.QueryRow(
		`SELECT id, slug, type, label, config, active, created_at FROM links WHERE slug = ?`, slug))
}

// GetLink returns a link by id.
func (db *DB) GetLink(id int64) (*models.Link, error) {
	return db.scanLink(db.QueryRow(
		`SELECT id, slug, type, label, config, active, created_at FROM links WHERE id = ?`, id))
}

func (db *DB) scanLink(row *sql.Row) (*models.Link, error) {
	l := &models.Link{}
	var cfg string
	var active int
	err := row.Scan(&l.ID, &l.Slug, &l.Type, &l.Label, &cfg, &active, &l.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	l.Active = active == 1
	if err := json.Unmarshal([]byte(cfg), &l.Config); err != nil {
		return nil, err
	}
	return l, nil
}

// ListLinks returns all links, newest first, with event counts.
func (db *DB) ListLinks() ([]*models.Link, error) {
	rows, err := db.Query(`
		SELECT l.id, l.slug, l.type, l.label, l.config, l.active, l.created_at,
		       COUNT(e.id) AS cnt, MAX(e.ts) AS last
		FROM links l
		LEFT JOIN events e ON e.link_id = l.id
		GROUP BY l.id
		ORDER BY l.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Link
	for rows.Next() {
		l := &models.Link{}
		var cfg string
		var active int
		// MAX(e.ts) is an aggregate expression, and the sqlite driver does not
		// reliably report its declared column type the way it does for a
		// plain column reference — so it can come back as a raw string
		// instead of being auto-converted to time.Time. Scan it as a
		// nullable string and parse it ourselves to avoid a driver-dependent
		// "unsupported Scan" error.
		var last sql.NullString
		if err := rows.Scan(&l.ID, &l.Slug, &l.Type, &l.Label, &cfg, &active,
			&l.CreatedAt, &l.EventCount, &last); err != nil {
			return nil, err
		}
		l.Active = active == 1
		if last.Valid {
			if t, err := parseSQLiteTime(last.String); err == nil {
				l.LastEvent = &t
			}
		}
		if err := json.Unmarshal([]byte(cfg), &l.Config); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SetLinkActive toggles a link's active flag.
func (db *DB) SetLinkActive(id int64, active bool) error {
	_, err := db.Exec(`UPDATE links SET active = ? WHERE id = ?`, boolToInt(active), id)
	return err
}

// DeleteLink removes a link and (via cascade) its events.
func (db *DB) DeleteLink(id int64) error {
	_, err := db.Exec(`DELETE FROM links WHERE id = ?`, id)
	return err
}

// ---- Events ----

// InsertEvent stores a capture record and returns its id.
func (db *DB) InsertEvent(e *models.Event) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO events (link_id, type, ip, country, region, city, lat, lon, isp, org, asn,
			user_agent, device, os, browser, referer, accept_language, headers_json,
			gps_lat, gps_lon, gps_accuracy)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.LinkID, e.Type, e.IP, e.Country, e.Region, e.City, e.Lat, e.Lon, e.ISP, e.Org, e.ASN,
		e.UserAgent, e.Device, e.OS, e.Browser, e.Referer, e.AcceptLanguage, e.HeadersJSON,
		nullFloat(e.GPSLat), nullFloat(e.GPSLon), nullFloat(e.GPSAccuracy))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AttachGPS updates an existing event with browser geolocation data.
func (db *DB) AttachGPS(id int64, lat, lon, accuracy float64, headersJSON string) error {
	_, err := db.Exec(`UPDATE events SET type = ?, gps_lat = ?, gps_lon = ?, gps_accuracy = ?, headers_json = ? WHERE id = ?`,
		models.EventGPS, lat, lon, accuracy, headersJSON, id)
	return err
}

// ListEvents returns events, optionally filtered by link, newest first, capped by limit.
func (db *DB) ListEvents(linkID int64, limit int) ([]*models.Event, error) {
	q := `
		SELECT e.id, e.link_id, e.type, e.ts, e.ip, e.country, e.region, e.city, e.lat, e.lon,
		       e.isp, e.org, e.asn, e.user_agent, e.device, e.os, e.browser, e.referer,
		       e.accept_language, e.headers_json, e.gps_lat, e.gps_lon, e.gps_accuracy,
		       l.slug, l.label
		FROM events e JOIN links l ON l.id = e.link_id`
	args := []any{}
	if linkID > 0 {
		q += ` WHERE e.link_id = ?`
		args = append(args, linkID)
	}
	q += ` ORDER BY e.ts DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Event
	for rows.Next() {
		e := &models.Event{}
		var gpsLat, gpsLon, gpsAcc sql.NullFloat64
		if err := rows.Scan(&e.ID, &e.LinkID, &e.Type, &e.Timestamp, &e.IP, &e.Country, &e.Region,
			&e.City, &e.Lat, &e.Lon, &e.ISP, &e.Org, &e.ASN, &e.UserAgent, &e.Device, &e.OS,
			&e.Browser, &e.Referer, &e.AcceptLanguage, &e.HeadersJSON,
			&gpsLat, &gpsLon, &gpsAcc, &e.LinkSlug, &e.LinkLabel); err != nil {
			return nil, err
		}
		if gpsLat.Valid {
			e.GPSLat = &gpsLat.Float64
		}
		if gpsLon.Valid {
			e.GPSLon = &gpsLon.Float64
		}
		if gpsAcc.Valid {
			e.GPSAccuracy = &gpsAcc.Float64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- Stats ----

// Stats is an aggregate snapshot for the dashboard.
type Stats struct {
	TotalLinks    int              `json:"total_links"`
	TotalEvents   int              `json:"total_events"`
	GPSCaptures   int              `json:"gps_captures"`
	UniqueIPs     int              `json:"unique_ips"`
	EventsByDay   []Bucket         `json:"events_by_day"`
	ByCountry     []Bucket         `json:"by_country"`
	ByDevice      []Bucket         `json:"by_device"`
	ByBrowser     []Bucket         `json:"by_browser"`
}

// Bucket is a label/count pair for charts.
type Bucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// GetStats computes dashboard aggregates. linkID > 0 scopes to one link.
func (db *DB) GetStats(linkID int64) (*Stats, error) {
	s := &Stats{}
	where := ""
	args := []any{}
	if linkID > 0 {
		where = ` WHERE link_id = ?`
		args = append(args, linkID)
	}

	_ = db.QueryRow(`SELECT COUNT(*) FROM links`).Scan(&s.TotalLinks)
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`+where, args...).Scan(&s.TotalEvents); err != nil {
		return nil, err
	}
	gpsWhere := ` WHERE type = 'gps'`
	if linkID > 0 {
		gpsWhere = ` WHERE link_id = ? AND type = 'gps'`
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM events`+gpsWhere, args...).Scan(&s.GPSCaptures)
	_ = db.QueryRow(`SELECT COUNT(DISTINCT ip) FROM events`+where, args...).Scan(&s.UniqueIPs)

	var err error
	if s.EventsByDay, err = db.buckets(
		`SELECT date(ts) AS l, COUNT(*) FROM events`+where+` GROUP BY l ORDER BY l`, args); err != nil {
		return nil, err
	}
	if s.ByCountry, err = db.buckets(
		`SELECT CASE WHEN country='' THEN 'Unknown' ELSE country END AS l, COUNT(*)
		 FROM events`+where+` GROUP BY l ORDER BY COUNT(*) DESC LIMIT 10`, args); err != nil {
		return nil, err
	}
	if s.ByDevice, err = db.buckets(
		`SELECT CASE WHEN device='' THEN 'Unknown' ELSE device END AS l, COUNT(*)
		 FROM events`+where+` GROUP BY l ORDER BY COUNT(*) DESC`, args); err != nil {
		return nil, err
	}
	if s.ByBrowser, err = db.buckets(
		`SELECT CASE WHEN browser='' THEN 'Unknown' ELSE browser END AS l, COUNT(*)
		 FROM events`+where+` GROUP BY l ORDER BY COUNT(*) DESC LIMIT 10`, args); err != nil {
		return nil, err
	}
	return s, nil
}

func (db *DB) buckets(query string, args []any) ([]Bucket, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Bucket{}
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Label, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullFloat(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}
