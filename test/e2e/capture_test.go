package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestRedirectCapture verifies a /s/{slug} hit logs a "click" event and
// 302s to the configured destination.
func TestRedirectCapture(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)
	a.createLink(t, client, url.Values{"type": {"redirect"}, "slug": {"rc1"}, "destination": {"https://example.com/dest"}}).Body.Close()

	anon := noRedirectClient(&http.Client{Jar: client.Jar})
	resp := a.get(t, anon, "/s/rc1")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("redirect: got %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "https://example.com/dest" {
		t.Fatalf("redirect Location = %q, want https://example.com/dest", loc)
	}

	events := a.eventsJSON(t, client, "")
	if !containsEventType(events, "click") {
		t.Fatalf("no 'click' event logged for /s/rc1: %+v", events)
	}
}

// TestPixelCapture verifies a pixel hit logs a "pixel_open" event and
// returns real image bytes.
func TestPixelCapture(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)
	a.createLink(t, client, url.Values{"type": {"pixel"}, "slug": {"px1"}}).Body.Close()

	resp := a.get(t, client, "/i/px1.png")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pixel: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("pixel content-type = %q, want image/png", ct)
	}
	body := bodyString(t, resp)
	if len(body) == 0 {
		t.Fatal("pixel response body is empty")
	}

	events := a.eventsJSON(t, client, "")
	if !containsEventType(events, "pixel_open") {
		t.Fatalf("no 'pixel_open' event logged: %+v", events)
	}
}

// TestGPSDecoyCapture verifies visiting the decoy page logs a "view" event,
// and posting coordinates attaches them to that same event (not a new one).
func TestGPSDecoyCapture(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)
	a.createLink(t, client, url.Values{"type": {"gps"}, "slug": {"gpsc1"}, "theme": {"cats"}}).Body.Close()

	resp := a.get(t, client, "/g/gpsc1")
	body := bodyString(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gps decoy page: got %d, want 200", resp.StatusCode)
	}
	eventID := extractEventID(t, body)

	loc := url.Values{}
	payload := `{"event_id":` + itoa(eventID) + `,"lat":12.5,"lon":77.5,"accuracy":10,"fingerprint":{"tz":"UTC"}}`
	req, err := http.NewRequest(http.MethodPost, a.srv.URL+"/g/gpsc1/loc", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	_ = loc
	postResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post gps: %v", err)
	}
	defer postResp.Body.Close()
	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("gps loc post: got %d, want 200", postResp.StatusCode)
	}

	events := a.eventsJSON(t, client, "")
	found := false
	for _, e := range events {
		if int64(e["ID"].(float64)) == eventID {
			if lat, ok := e["GPSLat"].(float64); !ok || lat != 12.5 {
				t.Fatalf("event %d GPSLat = %v, want 12.5", eventID, e["GPSLat"])
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("event id %d not found after GPS attach", eventID)
	}

	// Exactly one event should exist for this link — the GPS attach must
	// not have created a second one.
	if n := countEventsForLink(events); n != 1 {
		t.Fatalf("expected exactly 1 event after view+GPS-attach, got %d", n)
	}
}

// TestCloneCapture verifies visiting a Clone/Preview link's bounce page
// logs a "view" event and eventually serves the real target's HTML.
func TestCloneCapture(t *testing.T) {
	target := newTargetServer(t, `<!doctype html><html><head><title>Real Site</title></head><body><h1>Hello</h1></body></html>`)
	defer target.Close()

	a := newTestApp(t)
	client := a.authedClient(t)
	a.createLink(t, client, url.Values{"type": {"clone"}, "slug": {"cl1"}, "destination": {target.URL}}).Body.Close()

	// The bounce page itself.
	resp := a.get(t, client, "/p/cl1")
	body := bodyString(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clone bounce page: got %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "sw.js") {
		t.Fatal("clone bounce page doesn't reference the service worker script")
	}

	// The ?direct=1 fallback view (no service worker involved) should serve
	// the real target's content, unrewritten HTML aside from proxied URLs.
	viewResp := a.get(t, client, "/p/cl1/view?eid=1&direct=1")
	defer viewResp.Body.Close()
	viewBody := bodyString(t, viewResp)
	if !strings.Contains(viewBody, "Hello") {
		t.Fatalf("clone ?direct=1 view missing target content: %s", viewBody)
	}

	events := a.eventsJSON(t, client, "")
	if !containsEventType(events, "view") {
		t.Fatalf("no 'view' event logged for /p/cl1: %+v", events)
	}
}

// eventsJSON fetches the admin events API, optionally filtered by q.
func (a *testApp) eventsJSON(t *testing.T, client *http.Client, q string) []map[string]any {
	t.Helper()
	path := "/admin/api/events"
	if q != "" {
		path += "?q=" + url.QueryEscape(q)
	}
	resp := a.get(t, client, path)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events api: got %d, want 200", resp.StatusCode)
	}
	var out struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode events api: %v", err)
	}
	return out.Events
}

func containsEventType(events []map[string]any, typ string) bool {
	for _, e := range events {
		if e["Type"] == typ {
			return true
		}
	}
	return false
}

func countEventsForLink(events []map[string]any) int {
	return len(events)
}

// extractEventID pulls the numeric EVENT_ID out of the decoy page's inline
// script (const EVENT_ID = 123;).
func extractEventID(t *testing.T, html string) int64 {
	t.Helper()
	const marker = "EVENT_ID = "
	idx := strings.Index(html, marker)
	if idx == -1 {
		t.Fatalf("could not find EVENT_ID in decoy page HTML")
	}
	rest := html[idx+len(marker):]
	end := strings.IndexByte(rest, ';')
	if end == -1 {
		t.Fatalf("malformed EVENT_ID assignment in decoy page HTML")
	}
	id, err := parseInt(strings.TrimSpace(rest[:end]))
	if err != nil {
		t.Fatalf("parse EVENT_ID: %v", err)
	}
	return id
}
