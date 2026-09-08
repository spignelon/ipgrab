package handlers

import (
	"crypto/rand"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spignelon/ipgrab/internal/auth"
	"github.com/spignelon/ipgrab/internal/db"
	"github.com/spignelon/ipgrab/internal/models"
)

const slugAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// randomSlug returns a URL-safe random slug of length n.
func randomSlug(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	out := make([]byte, n)
	for i, c := range b {
		out[i] = slugAlphabet[int(c)%len(slugAlphabet)]
	}
	return string(out)
}

// ---- First-run setup ----

// Setup handles GET/POST /setup: claims the single admin account once.
func (h *Handler) Setup(w http.ResponseWriter, r *http.Request) {
	exists, err := h.DB.AdminExists()
	if err != nil {
		internalError(w, "setup: check admin exists", err)
		return
	}
	if exists {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodGet {
		h.render(w, "setup.html", map[string]any{})
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	pw := r.FormValue("password")
	confirm := r.FormValue("confirm")
	if username == "" || len(pw) < 8 || pw != confirm {
		h.render(w, "setup.html", map[string]any{
			"Error": "Username required and passwords must match (min 8 characters).",
		})
		return
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		internalError(w, "setup: hash password", err)
		return
	}
	if err := h.DB.CreateAdmin(username, hash); err != nil {
		internalError(w, "setup: create admin", err)
		return
	}
	if err := h.Auth.Login(w); err != nil {
		internalError(w, "setup: create session", err)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// ---- Login / logout ----

// Login handles GET/POST /login.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	exists, err := h.DB.AdminExists()
	if err != nil {
		internalError(w, "login: check admin exists", err)
		return
	}
	if !exists {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if h.Auth.IsAuthed(r) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodGet {
		h.render(w, "login.html", map[string]any{})
		return
	}

	admin, err := h.DB.GetAdmin()
	if err != nil {
		internalError(w, "login: get admin", err)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	pw := r.FormValue("password")
	if username != admin.Username || !auth.CheckPassword(admin.PasswordHash, pw) {
		h.render(w, "login.html", map[string]any{"Error": "Invalid credentials."})
		return
	}
	if err := h.Auth.Login(w); err != nil {
		internalError(w, "login: create session", err)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// Logout handles POST /logout.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	h.Auth.Logout(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---- Dashboard ----

// Dashboard handles GET /admin.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := h.DB.GetStats(0)
	if err != nil {
		internalError(w, "dashboard: get stats", err)
		return
	}
	recent, err := h.DB.ListEvents(0, 15)
	if err != nil {
		internalError(w, "dashboard: list events", err)
		return
	}
	h.render(w, "dashboard.html", map[string]any{
		"Nav":    "dashboard",
		"CSRF":   auth.CSRFToken(r),
		"Stats":  stats,
		"Recent": recent,
	})
}

// ---- Links CRUD ----

// LinksList handles GET /admin/links.
func (h *Handler) LinksList(w http.ResponseWriter, r *http.Request) {
	links, err := h.DB.ListLinks()
	if err != nil {
		internalError(w, "links list: query", err)
		return
	}
	h.render(w, "links.html", map[string]any{
		"Nav":     "links",
		"CSRF":    auth.CSRFToken(r),
		"Links":   links,
		"BaseURL": h.Cfg.BaseURL,
		"Error":   r.URL.Query().Get("err"),
	})
}

// CreateLink handles POST /admin/links.
func (h *Handler) CreateLink(w http.ResponseWriter, r *http.Request) {
	if !auth.VerifyCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		// Fall back to plain form parse for non-multipart submissions.
		_ = r.ParseForm()
	}

	typ := r.FormValue("type")
	switch typ {
	case models.TypeRedirect, models.TypePixel, models.TypeGPS, models.TypeClone:
	default:
		http.Redirect(w, r, "/admin/links?err=Invalid+link+type", http.StatusSeeOther)
		return
	}

	slug := strings.TrimSpace(r.FormValue("slug"))
	if slug == "" {
		slug = randomSlug(7)
	} else if !validSlug(slug) {
		http.Redirect(w, r, "/admin/links?err=Slug+may+only+contain+letters,+numbers,+-+and+_", http.StatusSeeOther)
		return
	}
	if taken, _ := h.DB.SlugExists(slug); taken {
		http.Redirect(w, r, "/admin/links?err=That+slug+is+already+taken", http.StatusSeeOther)
		return
	}

	cfg := models.LinkConfig{
		Destination: strings.TrimSpace(r.FormValue("destination")),
		Theme:       r.FormValue("theme"),
		Title:       r.FormValue("title"),
		Description: r.FormValue("description"),
		Image:       strings.TrimSpace(r.FormValue("og_image")),
	}

	// Handle a custom pixel image upload.
	if typ == models.TypePixel {
		if fname, err := h.saveUpload(r); err != nil {
			http.Redirect(w, r, "/admin/links?err="+urlEsc(err.Error()), http.StatusSeeOther)
			return
		} else if fname != "" {
			cfg.ImagePath = fname
		}
	}

	link := &models.Link{
		Slug:   slug,
		Type:   typ,
		Label:  strings.TrimSpace(r.FormValue("label")),
		Config: cfg,
		Active: true,
	}
	if _, err := h.DB.CreateLink(link); err != nil {
		http.Redirect(w, r, "/admin/links?err=Could+not+create+link", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/links", http.StatusSeeOther)
}

// saveUpload stores an uploaded "image" form file and returns its stored
// filename, or "" if none was provided.
func (h *Handler) saveUpload(r *http.Request) (string, error) {
	file, header, err := r.FormFile("image")
	if errors.Is(err, http.ErrMissingFile) || header == nil {
		return "", nil
	}
	if err != nil {
		return "", nil
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
	default:
		return "", fmt.Errorf("unsupported image type %q", ext)
	}
	name := randomSlug(12) + ext
	dst := filepath.Join(h.Cfg.UploadsDir(), name)
	out, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("could not save image")
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(file, 5<<20)); err != nil { // 5 MB cap
		return "", fmt.Errorf("could not save image")
	}
	return name, nil
}

// LinkDetail handles GET /admin/links/{id}.
func (h *Handler) LinkDetail(w http.ResponseWriter, r *http.Request) {
	id := atoi64(r.PathValue("id"))
	link, err := h.DB.GetLink(id)
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		internalError(w, "link detail: get link", err)
		return
	}
	events, err := h.DB.ListEvents(id, 500)
	if err != nil {
		internalError(w, "link detail: list events", err)
		return
	}
	stats, _ := h.DB.GetStats(id)

	// Build map points for the Leaflet map.
	type pt struct {
		Lat   float64 `json:"lat"`
		Lon   float64 `json:"lon"`
		Label string  `json:"label"`
		GPS   bool    `json:"gps"`
	}
	pts := []pt{}
	for _, e := range events {
		if e.BestLat() == 0 && e.BestLon() == 0 {
			continue
		}
		pts = append(pts, pt{
			Lat:   e.BestLat(),
			Lon:   e.BestLon(),
			Label: fmt.Sprintf("%s — %s (%s)", e.IP, e.City, e.Timestamp.Format("2006-01-02 15:04")),
			GPS:   e.HasGPS(),
		})
	}

	h.render(w, "link_detail.html", map[string]any{
		"Nav":      "links",
		"CSRF":     auth.CSRFToken(r),
		"Link":     link,
		"Events":   events,
		"Stats":    stats,
		"BaseURL":  h.Cfg.BaseURL,
		"ShareURL": h.shareURL(link),
		"Points":   pts,
	})
}

// ToggleLink handles POST /admin/links/{id}/toggle.
func (h *Handler) ToggleLink(w http.ResponseWriter, r *http.Request) {
	if !auth.VerifyCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	id := atoi64(r.PathValue("id"))
	link, err := h.DB.GetLink(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = h.DB.SetLinkActive(id, !link.Active)
	http.Redirect(w, r, "/admin/links", http.StatusSeeOther)
}

// DeleteLink handles POST /admin/links/{id}/delete.
func (h *Handler) DeleteLink(w http.ResponseWriter, r *http.Request) {
	if !auth.VerifyCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	id := atoi64(r.PathValue("id"))
	// Best-effort cleanup of a custom pixel image.
	if link, err := h.DB.GetLink(id); err == nil && link.Config.ImagePath != "" {
		_ = os.Remove(filepath.Join(h.Cfg.UploadsDir(), filepath.Base(link.Config.ImagePath)))
	}
	_ = h.DB.DeleteLink(id)
	http.Redirect(w, r, "/admin/links", http.StatusSeeOther)
}

// ---- Stats API + CSV ----

// StatsAPI handles GET /admin/api/stats(.json), optionally scoped by ?link=id.
func (h *Handler) StatsAPI(w http.ResponseWriter, r *http.Request) {
	linkID := atoi64(r.URL.Query().Get("link"))
	stats, err := h.DB.GetStats(linkID)
	if err != nil {
		internalError(w, "stats api: get stats", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

// EventsCSV handles GET /admin/events.csv, optionally scoped by ?link=id.
func (h *Handler) EventsCSV(w http.ResponseWriter, r *http.Request) {
	linkID := atoi64(r.URL.Query().Get("link"))
	events, err := h.DB.ListEvents(linkID, 0)
	if err != nil {
		internalError(w, "csv export: list events", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="ipgrab-events-%s.csv"`, time.Now().Format("20060102-150405")))

	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"timestamp", "link_slug", "link_label", "type", "ip", "country", "region", "city",
		"isp", "org", "asn", "device", "os", "browser", "gps_lat", "gps_lon", "gps_accuracy",
		"accept_language", "user_agent",
	})
	for _, e := range events {
		_ = cw.Write([]string{
			e.Timestamp.Format(time.RFC3339), e.LinkSlug, e.LinkLabel, e.Type, e.IP, e.Country,
			e.Region, e.City, e.ISP, e.Org, e.ASN, e.Device, e.OS, e.Browser,
			floatOrEmpty(e.GPSLat), floatOrEmpty(e.GPSLon), floatOrEmpty(e.GPSAccuracy),
			e.AcceptLanguage, e.UserAgent,
		})
	}
}

// shareURL returns the public capture URL for a link.
func (h *Handler) shareURL(l *models.Link) string {
	switch l.Type {
	case models.TypeRedirect:
		return h.Cfg.BaseURL + "/s/" + l.Slug
	case models.TypePixel:
		return h.Cfg.BaseURL + "/i/" + l.Slug + ".png"
	case models.TypeGPS:
		return h.Cfg.BaseURL + "/g/" + l.Slug
	case models.TypeClone:
		return h.Cfg.BaseURL + "/p/" + l.Slug
	}
	return h.Cfg.BaseURL
}

func validSlug(s string) bool {
	if len(s) > 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func floatOrEmpty(f *float64) string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf("%.6f", *f)
}

func urlEsc(s string) string {
	return strings.ReplaceAll(s, " ", "+")
}
