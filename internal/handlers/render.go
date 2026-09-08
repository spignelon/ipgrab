// Package handlers implements the public capture routes and the admin dashboard.
package handlers

import (
	"encoding/json"
	"html/template"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spignelon/ipgrab/internal/auth"
	"github.com/spignelon/ipgrab/internal/config"
	"github.com/spignelon/ipgrab/internal/db"
	"github.com/spignelon/ipgrab/internal/geoip"
	"github.com/spignelon/ipgrab/internal/models"
	"github.com/spignelon/ipgrab/web"
)

// Handler bundles the dependencies shared across all routes.
type Handler struct {
	DB   *db.DB
	Cfg  *config.Config
	Auth *auth.Manager
	Geo  *geoip.Client
	tmpl *template.Template

	// concealed caches the "conceal mode" setting in memory so every request
	// (including unauthenticated ones like /login and /favicon.ico) can check
	// it without a DB round trip. Kept in sync via SetConcealed on toggle.
	concealed atomic.Bool
}

// New constructs a Handler and parses all templates.
func New(database *db.DB, cfg *config.Config, am *auth.Manager, geo *geoip.Client) (*Handler, error) {
	funcs := template.FuncMap{
		"fmtTime": func(t time.Time) string { return t.Format("2006-01-02 15:04:05 MST") },
		"fmtDate": func(t time.Time) string { return t.Format("Jan 2, 2006") },
		"since":   func(t time.Time) string { return humanize(time.Since(t)) },
		"typeBadge": func(s string) string {
			switch s {
			case "redirect":
				return "Redirect"
			case "pixel":
				return "Pixel"
			case "gps":
				return "GPS Decoy"
			case "clone":
				return "Clone/Preview"
			}
			return s
		},
		"shareURL": func(base string, l *models.Link) string {
			switch l.Type {
			case models.TypeRedirect:
				return base + "/s/" + l.Slug
			case models.TypePixel:
				return base + "/i/" + l.Slug + ".png"
			case models.TypeGPS:
				return base + "/g/" + l.Slug
			case models.TypeClone:
				return base + "/p/" + l.Slug
			}
			return base
		},
		"toJSON": func(v any) template.JS {
			b, err := json.Marshal(v)
			if err != nil {
				return "null"
			}
			return template.JS(b)
		},
	}
	t, err := template.New("").Funcs(funcs).ParseFS(web.Templates, "templates/*.html")
	if err != nil {
		return nil, err
	}
	h := &Handler{DB: database, Cfg: cfg, Auth: am, Geo: geo, tmpl: t}
	if enabled, err := database.ConcealEnabled(); err != nil {
		log.Printf("WARNING: could not load conceal-mode setting, defaulting to off: %v", err)
	} else {
		h.concealed.Store(enabled)
	}
	return h, nil
}

// internalError logs the real cause server-side (so it shows up in `docker
// logs`) and returns a generic 500 to the client.
func internalError(w http.ResponseWriter, context string, err error) {
	log.Printf("%s: %v", context, err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// Concealed reports whether conceal mode is currently on.
func (h *Handler) Concealed() bool { return h.concealed.Load() }

// SetConcealed updates the in-memory conceal-mode cache. Call this right
// after persisting the new value with DB.SetConcealEnabled.
func (h *Handler) SetConcealed(v bool) { h.concealed.Store(v) }

// render executes a named template with a base layout. Every page gets a
// "Concealed" data key automatically so templates can switch their
// title/favicon/branding without every call site remembering to pass it.
func (h *Handler) render(w http.ResponseWriter, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["Concealed"]; !ok {
		data["Concealed"] = h.Concealed()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// clientIP resolves the real client IP, honouring proxy headers only when
// TRUST_PROXY is enabled.
func (h *Handler) clientIP(r *http.Request) string {
	if h.Cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// First entry is the original client.
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
		if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
			return xr
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// humanize renders a duration as a compact relative string.
func humanize(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return itoa(int(d.Hours())) + "h ago"
	default:
		return itoa(int(d.Hours()/24)) + "d ago"
	}
}

func itoa(n int) string {
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
