// Package auth handles the single-admin login: password hashing, session
// cookies, CSRF tokens, and the middleware that guards admin routes.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/spignelon/ipgrab/internal/db"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie = "ipgrab_session"
	csrfCookie    = "ipgrab_csrf"
	// SessionTTL is how long a login lasts.
	SessionTTL = 7 * 24 * time.Hour
)

// Manager wires auth helpers to the database and cookie settings.
type Manager struct {
	DB           *db.DB
	CookieSecure bool
}

// HashPassword returns a bcrypt hash of the given password.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword verifies a password against a bcrypt hash.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// token returns a URL-safe random token of n bytes.
func token(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Login creates a session, sets the session + CSRF cookies, and returns nil on success.
func (m *Manager) Login(w http.ResponseWriter) error {
	tok := token(32)
	if err := m.DB.CreateSession(tok, SessionTTL); err != nil {
		return err
	}
	m.setCookie(w, sessionCookie, tok, SessionTTL, true)
	// CSRF token is readable by templates (not HttpOnly) so forms can echo it.
	m.setCookie(w, csrfCookie, token(24), SessionTTL, false)
	return nil
}

// Logout clears the current session server-side and expires the cookies.
func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = m.DB.DeleteSession(c.Value)
	}
	m.clearCookie(w, sessionCookie)
	m.clearCookie(w, csrfCookie)
}

// IsAuthed reports whether the request carries a valid session.
func (m *Manager) IsAuthed(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	ok, err := m.DB.SessionValid(c.Value)
	return err == nil && ok
}

// CSRFToken returns the CSRF token from the request cookie, or "".
func CSRFToken(r *http.Request) string {
	if c, err := r.Cookie(csrfCookie); err == nil {
		return c.Value
	}
	return ""
}

// RequireAuth wraps a handler, redirecting unauthenticated users to /login.
func (m *Manager) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.IsAuthed(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// VerifyCSRF constant-time compares the form token against the cookie for unsafe methods.
func VerifyCSRF(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	cookie := CSRFToken(r)
	form := r.FormValue("csrf_token")
	if cookie == "" || form == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie), []byte(form)) == 1
}

func (m *Manager) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  time.Now().Add(ttl),
		HttpOnly: httpOnly,
		Secure:   m.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *Manager) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}
