package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spignelon/ipgrab/internal/db"
	"github.com/spignelon/ipgrab/internal/models"
	"github.com/spignelon/ipgrab/internal/uaparse"
	"github.com/spignelon/ipgrab/web"
)

// transparent1x1PNG is a 1x1 fully transparent PNG used as the default pixel.
var transparent1x1PNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x62, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// Favicon handles GET /favicon.ico. Outside conceal mode this behaves exactly
// as before (no favicon was ever served, so plain 404 — no behavior change).
// In conceal mode it serves the real Nextcloud favicon (web.ConcealFavicon),
// covering browsers/crawlers that request /favicon.ico directly regardless
// of the page's own <link rel="icon"> tag.
func (h *Handler) Favicon(w http.ResponseWriter, r *http.Request) {
	if !h.Concealed() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(web.ConcealFavicon)
}

// concealAsset writes one embedded conceal-mode asset (real Nextcloud logo,
// core favicon, or login background), or 404s when conceal mode is off,
// matching how every other conceal-only surface behaves.
func (h *Handler) concealAsset(w http.ResponseWriter, contentType string, data []byte) {
	if !h.Concealed() {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
}

// These three routes mirror Nextcloud's own real asset URLs — deliberately
// not served under /static/, and never named "conceal" in the URL itself,
// since that would spell out the disguise to anyone who views page source.

// ConcealLogo handles GET /core/img/logo/logo.svg.
func (h *Handler) ConcealLogo(w http.ResponseWriter, r *http.Request) {
	h.concealAsset(w, "image/svg+xml", web.ConcealLogo)
}

// ConcealCoreFavicon handles GET /core/img/favicon.svg — the in-page icon
// link the login page itself references (distinct from /favicon.ico above,
// which browsers/crawlers request directly regardless of page content).
func (h *Handler) ConcealCoreFavicon(w http.ResponseWriter, r *http.Request) {
	h.concealAsset(w, "image/svg+xml", web.ConcealFavicon)
}

// ConcealBackground handles GET /apps/theming/img/background/jo-myoung-hee-fluid.webp
// — Nextcloud's real default login-screen background image path.
func (h *Handler) ConcealBackground(w http.ResponseWriter, r *http.Request) {
	h.concealAsset(w, "image/webp", web.ConcealBackground)
}

// capture builds, enriches, and stores an event for a link, returning the new
// event id (useful for the GPS flow, which later attaches coordinates).
func (h *Handler) capture(r *http.Request, link *models.Link, eventType string) int64 {
	ip := h.clientIP(r)
	geo := h.Geo.Lookup(ip)
	ua := uaparse.Parse(r.UserAgent())

	// Snapshot notable headers for later inspection.
	hdr := map[string]string{
		"X-Forwarded-For": r.Header.Get("X-Forwarded-For"),
		"X-Real-IP":       r.Header.Get("X-Real-IP"),
		"DNT":             r.Header.Get("DNT"),
		"Sec-CH-UA":       r.Header.Get("Sec-CH-UA"),
		"Sec-CH-UA-Mobile": r.Header.Get("Sec-CH-UA-Mobile"),
		"Sec-CH-UA-Platform": r.Header.Get("Sec-CH-UA-Platform"),
	}
	hdrJSON, _ := json.Marshal(hdr)

	e := &models.Event{
		LinkID:         link.ID,
		Type:           eventType,
		IP:             ip,
		Country:        geo.Country,
		Region:         geo.Region,
		City:           geo.City,
		Lat:            geo.Lat,
		Lon:            geo.Lon,
		ISP:            geo.ISP,
		Org:            geo.Org,
		ASN:            geo.ASN,
		UserAgent:      r.UserAgent(),
		Device:         ua.Device,
		OS:             ua.OS,
		Browser:        ua.Browser,
		Referer:        r.Referer(),
		AcceptLanguage: r.Header.Get("Accept-Language"),
		HeadersJSON:    string(hdrJSON),
	}
	id, err := h.DB.InsertEvent(e)
	if err != nil {
		log.Printf("capture: insert event: %v", err)
		return 0
	}
	return id
}

// lookupActive fetches a link by slug and 404s if missing or disabled.
func (h *Handler) lookupActive(w http.ResponseWriter, slug string) *models.Link {
	link, err := h.DB.GetLinkBySlug(slug)
	if errors.Is(err, db.ErrNotFound) || (link != nil && !link.Active) {
		http.NotFound(w, nil)
		return nil
	}
	if err != nil {
		internalError(w, "lookup link", err)
		return nil
	}
	return link
}

// noStore sets headers to discourage caching of capture responses.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// Redirect handles GET /s/{slug}: logs a click then 302s to the destination.
func (h *Handler) Redirect(w http.ResponseWriter, r *http.Request) {
	link := h.lookupActive(w, r.PathValue("slug"))
	if link == nil {
		return
	}
	if link.Type != models.TypeRedirect {
		http.NotFound(w, nil)
		return
	}
	h.capture(r, link, models.EventClick)
	dest := link.Config.Destination
	if dest == "" {
		dest = "https://example.com"
	}
	noStore(w)
	http.Redirect(w, r, dest, http.StatusFound)
}

// Pixel handles GET /i/{slug}.png: logs an open then returns the image bytes.
func (h *Handler) Pixel(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSuffix(r.PathValue("slug"), ".png")
	link := h.lookupActive(w, slug)
	if link == nil {
		return
	}
	if link.Type != models.TypePixel {
		http.NotFound(w, nil)
		return
	}
	h.capture(r, link, models.EventPixelOpen)
	noStore(w)

	// Serve a custom uploaded image if configured, else the transparent 1x1.
	if link.Config.ImagePath != "" {
		path := filepath.Join(h.Cfg.UploadsDir(), filepath.Base(link.Config.ImagePath))
		if f, err := os.Open(path); err == nil {
			defer f.Close()
			w.Header().Set("Content-Type", contentTypeFor(path))
			_, _ = io.Copy(w, f)
			return
		}
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(transparent1x1PNG)
}

// GPSPage handles GET /g/{slug}: renders the decoy page which requests geolocation.
func (h *Handler) GPSPage(w http.ResponseWriter, r *http.Request) {
	link := h.lookupActive(w, r.PathValue("slug"))
	if link == nil {
		return
	}
	if link.Type != models.TypeGPS {
		http.NotFound(w, nil)
		return
	}
	// Log the initial view; the browser JS will POST coordinates to attach.
	eventID := h.capture(r, link, models.EventView)
	noStore(w)
	theme := link.Config.Theme
	if theme == "" {
		theme = "cats"
	}
	h.render(w, "decoy.html", map[string]any{
		"Slug":     link.Slug,
		"EventID":  eventID,
		"Theme":    theme,
		"Redirect": link.Config.Destination,
		"Label":    link.Label,
	})
}

// gpsPayload is the JSON body posted by the decoy page.
type gpsPayload struct {
	EventID     int64             `json:"event_id"`
	Lat         float64           `json:"lat"`
	Lon         float64           `json:"lon"`
	Accuracy    float64           `json:"accuracy"`
	Fingerprint map[string]string `json:"fingerprint"`
}

// GPSCollect handles POST /g/{slug}/loc: attaches precise coordinates to the event.
func (h *Handler) GPSCollect(w http.ResponseWriter, r *http.Request) {
	link := h.lookupActive(w, r.PathValue("slug"))
	if link == nil {
		return
	}
	var p gpsPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&p); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	fp, _ := json.Marshal(p.Fingerprint)
	if p.EventID > 0 {
		if err := h.DB.AttachGPS(p.EventID, p.Lat, p.Lon, p.Accuracy, string(fp)); err != nil {
			log.Printf("gps attach: %v", err)
		}
	} else {
		// No prior event id (edge case): create a fresh GPS event.
		id := h.capture(r, link, models.EventGPS)
		if id > 0 {
			_ = h.DB.AttachGPS(id, p.Lat, p.Lon, p.Accuracy, string(fp))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// ClonePage handles GET /p/{slug}: renders a rich link-preview landing page and
// logs a view; the click-through logs a click and redirects.
func (h *Handler) ClonePage(w http.ResponseWriter, r *http.Request) {
	link := h.lookupActive(w, r.PathValue("slug"))
	if link == nil {
		return
	}
	if link.Type != models.TypeClone {
		http.NotFound(w, nil)
		return
	}
	// A ?go=1 query means the visitor clicked through: log a click and redirect.
	if r.URL.Query().Get("go") == "1" {
		h.capture(r, link, models.EventClick)
		noStore(w)
		dest := link.Config.Destination
		if dest == "" {
			dest = "https://example.com"
		}
		http.Redirect(w, r, dest, http.StatusFound)
		return
	}
	h.capture(r, link, models.EventView)
	noStore(w)
	h.render(w, "clone.html", map[string]any{
		"Slug":        link.Slug,
		"Title":       orDefault(link.Config.Title, link.Label),
		"Description": link.Config.Description,
		"Image":       link.Config.Image,
		"Destination": link.Config.Destination,
		"BaseURL":     h.Cfg.BaseURL,
	})
}

func contentTypeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// atoi64 parses a base-10 int64, returning 0 on error.
func atoi64(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
