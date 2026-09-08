// Command ipgrab is a self-hosted IP & location intelligence toolkit for
// authorized security assessments. See README.md for the usage disclaimer.
package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spignelon/ipgrab/internal/auth"
	"github.com/spignelon/ipgrab/internal/config"
	"github.com/spignelon/ipgrab/internal/db"
	"github.com/spignelon/ipgrab/internal/geoip"
	"github.com/spignelon/ipgrab/internal/handlers"
	"github.com/spignelon/ipgrab/web"
)

func main() {
	cfg := config.Load()

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer database.Close()
	_ = database.PurgeExpiredSessions()

	am := &auth.Manager{DB: database, CookieSecure: cfg.CookieSecure}
	geo := geoip.New()

	h, err := handlers.New(database, cfg, am, geo)
	if err != nil {
		log.Fatalf("handlers: %v", err)
	}

	mux := http.NewServeMux()

	// Static assets (embedded).
	staticFS, _ := fs.Sub(web.Static, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Public capture surfaces.
	mux.HandleFunc("GET /s/{slug}", h.Redirect)
	mux.HandleFunc("GET /i/{slug}", h.Pixel)
	mux.HandleFunc("GET /g/{slug}", h.GPSPage)
	mux.HandleFunc("POST /g/{slug}/loc", h.GPSCollect)
	mux.HandleFunc("GET /p/{slug}", h.ClonePage)

	// Auth + setup.
	mux.HandleFunc("/setup", h.Setup)
	mux.HandleFunc("/login", h.Login)
	mux.HandleFunc("POST /logout", h.Logout)

	// Admin (guarded).
	mux.HandleFunc("GET /admin", am.RequireAuth(h.Dashboard))
	mux.HandleFunc("GET /admin/links", am.RequireAuth(h.LinksList))
	mux.HandleFunc("POST /admin/links", am.RequireAuth(h.CreateLink))
	mux.HandleFunc("GET /admin/links/{id}", am.RequireAuth(h.LinkDetail))
	mux.HandleFunc("POST /admin/links/{id}/toggle", am.RequireAuth(h.ToggleLink))
	mux.HandleFunc("POST /admin/links/{id}/delete", am.RequireAuth(h.DeleteLink))
	mux.HandleFunc("GET /admin/api/stats", am.RequireAuth(h.StatsAPI))
	mux.HandleFunc("GET /admin/events.csv", am.RequireAuth(h.EventsCSV))

	// Root: send to dashboard (or setup/login as appropriate).
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	// Graceful shutdown.
	go func() {
		log.Printf("IPGrab listening on :%s (base URL %s)", cfg.Port, cfg.BaseURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// logRequests is a minimal access-log middleware.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
