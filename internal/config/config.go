// Package config loads runtime configuration from environment variables.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds all runtime settings for the server.
type Config struct {
	Port         string // TCP port to listen on
	BaseURL      string // public base URL, used when rendering shareable links (no trailing slash)
	DataDir      string // directory for the sqlite db and uploaded images
	SessionKey   []byte // secret used to sign session cookies
	TrustProxy   bool   // honour X-Forwarded-For / X-Real-IP when true
	CookieSecure bool   // mark session cookie Secure (set when serving over HTTPS)
}

// Load reads configuration from the environment, applying sensible defaults so
// the app runs out of the box for local testing.
func Load() *Config {
	c := &Config{
		Port:         env("PORT", "8080"),
		BaseURL:      strings.TrimRight(env("BASE_URL", ""), "/"),
		DataDir:      env("DATA_DIR", "./data"),
		TrustProxy:   envBool("TRUST_PROXY", false),
		CookieSecure: envBool("COOKIE_SECURE", false),
	}

	if secret := os.Getenv("SESSION_SECRET"); secret != "" {
		c.SessionKey = []byte(secret)
	} else {
		// Generate an ephemeral key so the app still runs, but warn: sessions
		// will not survive a restart and this is unsafe for production.
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			log.Fatalf("config: cannot generate session secret: %v", err)
		}
		c.SessionKey = []byte(hex.EncodeToString(b))
		log.Println("WARNING: SESSION_SECRET not set — using a random ephemeral key. " +
			"Sessions will be invalidated on restart. Set SESSION_SECRET in production.")
	}

	if c.BaseURL == "" {
		c.BaseURL = "http://localhost:" + c.Port
		log.Printf("WARNING: BASE_URL not set — defaulting to %s. Generated links will use this.", c.BaseURL)
	}

	if err := os.MkdirAll(c.UploadsDir(), 0o750); err != nil {
		log.Fatalf("config: cannot create data dir %q: %v", c.DataDir, err)
	}
	return c
}

// DBPath returns the full path to the sqlite database file.
func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "ipgrab.db") }

// UploadsDir returns the directory where custom pixel images are stored.
func (c *Config) UploadsDir() string { return filepath.Join(c.DataDir, "uploads") }

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
