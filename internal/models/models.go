// Package models defines the core data structures persisted by IPGrab.
package models

import "time"

// Link types.
const (
	TypeRedirect = "redirect" // short link that logs then 302-redirects
	TypePixel    = "pixel"    // 1x1 or custom image, for email open tracking
	TypeGPS      = "gps"      // decoy page that requests browser geolocation
	TypeClone    = "clone"    // rich link-preview page with a click-through
)

// Event types.
const (
	EventView      = "view"       // page viewed (gps/clone landing)
	EventClick     = "click"      // redirect/clone click-through
	EventPixelOpen = "pixel_open" // tracking pixel loaded
	EventGPS       = "gps"        // precise geolocation captured
)

// Admin is the single owner account. Presence of a row means the instance is claimed.
type Admin struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// Session is a server-side login session keyed by an opaque token stored in a cookie.
type Session struct {
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// LinkConfig holds type-specific settings, serialised to JSON in the links table.
type LinkConfig struct {
	Destination string `json:"destination,omitempty"` // redirect target URL, or clone: origin page to live-proxy
	ImagePath   string `json:"image_path,omitempty"`  // pixel: custom uploaded image filename (empty = builtin 1x1)
	Theme       string `json:"theme,omitempty"`       // gps decoy theme (e.g. "cats")
	Title       string `json:"title,omitempty"`       // clone: OG title override (unused by the live proxy, kept for legacy data)
	Description string `json:"description,omitempty"` // clone: OG description override (unused by the live proxy)
	Image       string `json:"image,omitempty"`       // clone: OG image override (unused by the live proxy)

	ExpiresAt       string `json:"expires_at,omitempty"`       // RFC3339; empty = no time-based expiry
	MaxClicks       int    `json:"max_clicks,omitempty"`       // 0 = unlimited
	CloneURL        string `json:"clone_url,omitempty"`        // gps decoy: optional page to live-proxy instead of the theme page
	Channel         string `json:"channel,omitempty"`          // per-link ntfy topic / gotify token override for webhook notifications
	ExpiredNotified bool   `json:"expired_notified,omitempty"` // set once the one-time "expired" webhook has fired
}

// Link is a generated capture surface.
type Link struct {
	ID        int64
	Slug      string
	Type      string
	Label     string
	Config    LinkConfig
	Active    bool
	CreatedAt time.Time

	// EventCount is populated by list queries for convenience; not a stored column.
	EventCount int
	LastEvent  *time.Time

	// Expired is computed at read time (time or click-count expiry); not a stored column.
	Expired bool
}

// DisplayName returns the link's label, or its numeric id as a fallback
// "serial number" when no label was set.
func (l *Link) DisplayName() string {
	if l.Label != "" {
		return l.Label
	}
	return "#" + itoa64(l.ID)
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Event is a single capture record.
type Event struct {
	ID             int64
	LinkID         int64
	Type           string
	Timestamp      time.Time
	IP             string
	Country        string
	Region         string
	City           string
	Lat            float64 // IP-derived latitude
	Lon            float64 // IP-derived longitude
	ISP            string
	Org            string
	ASN            string
	UserAgent      string
	Device         string
	OS             string
	Browser        string
	Referer        string
	AcceptLanguage string
	HeadersJSON    string // JSON blob of notable request headers + JS fingerprint

	// GPS fields are only set for EventGPS records.
	GPSLat      *float64
	GPSLon      *float64
	GPSAccuracy *float64

	// SlugLabel is populated by joins for display; not a stored column.
	LinkSlug  string
	LinkLabel string
}

// HasGPS reports whether this event carries precise browser geolocation.
func (e *Event) HasGPS() bool { return e.GPSLat != nil && e.GPSLon != nil }

// BestLat returns GPS latitude if present, otherwise the IP-derived latitude.
func (e *Event) BestLat() float64 {
	if e.GPSLat != nil {
		return *e.GPSLat
	}
	return e.Lat
}

// BestLon returns GPS longitude if present, otherwise the IP-derived longitude.
func (e *Event) BestLon() float64 {
	if e.GPSLon != nil {
		return *e.GPSLon
	}
	return e.Lon
}
