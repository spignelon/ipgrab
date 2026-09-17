// Package notify sends click/expiry/GPS-capture notifications to a
// self-hosted ntfy or Gotify server, as configured from the admin Settings
// page. Sending is always best-effort: failures are logged, never surfaced
// to the visitor whose request triggered the notification.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/spignelon/ipgrab/internal/netguard"
)

// Supported webhook backend types.
const (
	TypeNone   = "none"
	TypeNtfy   = "ntfy"
	TypeGotify = "gotify"
)

// Priority names, shared across ntfy (string) and gotify (int) backends.
const (
	PriorityMin     = "min"
	PriorityLow     = "low"
	PriorityDefault = "default"
	PriorityHigh    = "high"
	PriorityUrgent  = "urgent"
)

// Config holds the current webhook + GPS-alert settings, loaded from the
// settings table and cached on the Handler.
type Config struct {
	Type     string // "none" | "ntfy" | "gotify"
	URL      string // base server URL, e.g. https://ntfy.sh or https://gotify.example.com
	Topic    string // ntfy topic
	Token    string // gotify application token
	Priority string // default priority for hit notifications

	// ntfy access control. ntfy servers can require auth for publishing:
	// either an access token (sent as "Authorization: Bearer <token>") or a
	// username/password pair (sent as HTTP Basic auth). AuthToken takes
	// precedence when both are set. Neither is required for a public/open
	// ntfy topic. Gotify's own auth is the application Token above.
	AuthToken string
	AuthUser  string
	AuthPass  string

	OnHit bool // whether link hits should notify at all

	GPSAlertEnabled  bool // separate high-priority alert for GPS captures
	GPSAlertPriority string
}

// Enabled reports whether a webhook backend is configured at all.
func (c Config) Enabled() bool {
	return c.Type == TypeNtfy || c.Type == TypeGotify
}

// The server URL is entirely admin-configurable (Settings), which makes it
// exactly the same shape of SSRF vector as the live-proxy clone engine's
// destination field — guard it the same way, via the shared netguard
// transport, rather than making an unrestricted request to it.
var httpClient = &http.Client{Timeout: 4 * time.Second, Transport: netguard.Transport()}

// SendHit fires a normal-priority notification for a link click/view/pixel
// event, if OnHit is enabled. channelOverride (a link's own Config.Channel)
// takes precedence over the global topic/token when non-empty.
func (c Config) SendHit(title, message, channelOverride string) {
	if !c.Enabled() || !c.OnHit {
		return
	}
	c.send(title, message, orDefault(c.Priority, PriorityDefault), channelOverride)
}

// SendGPSAlert fires a dedicated high-priority notification for a real GPS
// fix, if the separate GPS alert is enabled.
func (c Config) SendGPSAlert(title, message, channelOverride string) {
	if !c.Enabled() || !c.GPSAlertEnabled {
		return
	}
	c.send(title, message, orDefault(c.GPSAlertPriority, PriorityUrgent), channelOverride)
}

// SendExpired fires a one-time notification when a link becomes expired.
func (c Config) SendExpired(title, message, channelOverride string) {
	if !c.Enabled() {
		return
	}
	c.send(title, message, orDefault(c.Priority, PriorityDefault), channelOverride)
}

// Test sends a single notification to verify the current settings, ignoring
// the OnHit/GPSAlert toggles (those gate automatic notifications, not the
// manual test button).
func (c Config) Test() error {
	if !c.Enabled() {
		return fmt.Errorf("no webhook backend configured")
	}
	return c.sendErr("IPGrab test notification", "If you can see this, your webhook is configured correctly.",
		orDefault(c.Priority, PriorityDefault), "")
}

func (c Config) send(title, message, priority, channelOverride string) {
	go func() {
		if err := c.sendErr(title, message, priority, channelOverride); err != nil {
			log.Printf("notify: send: %v", err)
		}
	}()
}

func (c Config) sendErr(title, message, priority, channelOverride string) error {
	switch c.Type {
	case TypeNtfy:
		return c.sendNtfy(title, message, priority, channelOverride)
	case TypeGotify:
		return c.sendGotify(title, message, priority, channelOverride)
	default:
		return fmt.Errorf("unknown webhook type %q", c.Type)
	}
}

func (c Config) sendNtfy(title, message, priority, channelOverride string) error {
	topic := c.Topic
	if channelOverride != "" {
		topic = channelOverride
	}
	if c.URL == "" || topic == "" {
		return fmt.Errorf("ntfy: url and topic are required")
	}
	url := strings.TrimRight(c.URL, "/") + "/" + strings.TrimLeft(topic, "/")
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(message))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)
	req.Header.Set("Priority", priority)
	c.applyNtfyAuth(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy: server returned %s", resp.Status)
	}
	return nil
}

// applyNtfyAuth sets Authorization on an outgoing ntfy request when the
// server requires it: a bearer access token takes precedence, else HTTP
// Basic auth with the configured username/password, else no auth header
// (fine for a public/open topic).
func (c Config) applyNtfyAuth(req *http.Request) {
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	} else if c.AuthUser != "" || c.AuthPass != "" {
		req.SetBasicAuth(c.AuthUser, c.AuthPass)
	}
}

func (c Config) sendGotify(title, message, priority, channelOverride string) error {
	token := c.Token
	if channelOverride != "" {
		token = channelOverride
	}
	if c.URL == "" || token == "" {
		return fmt.Errorf("gotify: url and token are required")
	}
	body, err := json.Marshal(map[string]any{
		"title":    title,
		"message":  message,
		"priority": gotifyPriority(priority),
	})
	if err != nil {
		return err
	}
	url := strings.TrimRight(c.URL, "/") + "/message?token=" + token
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("gotify: server returned %s", resp.Status)
	}
	return nil
}

// gotifyPriority maps our named priorities to Gotify's 0-10 integer scale.
func gotifyPriority(p string) int {
	switch p {
	case PriorityMin:
		return 0
	case PriorityLow:
		return 2
	case PriorityHigh:
		return 6
	case PriorityUrgent:
		return 9
	default:
		return 4
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
