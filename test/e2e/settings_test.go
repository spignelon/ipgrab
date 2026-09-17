package e2e

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestConcealModeToggle exercises the conceal-mode setting end to end: it
// starts off, flips on (nav labels + login page change), then off again.
func TestConcealModeToggle(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	settings := bodyString(t, a.get(t, client, "/admin/settings"))
	if !strings.Contains(settings, "Conceal mode is OFF") {
		t.Fatalf("expected conceal mode OFF by default, settings page: %s", settings)
	}

	a.postForm(t, client, "/admin/settings/conceal", nil).Body.Close()

	settings = bodyString(t, a.get(t, client, "/admin/settings"))
	if !strings.Contains(settings, "Conceal mode is ON") {
		t.Fatal("conceal mode did not turn ON after toggling")
	}

	// The nav should now read "Links", not "Files" (see the earlier session
	// note: conceal mode disguises Dashboard/Activity/Settings but "Files"
	// for Links was confusing, so that one label was fixed to stay literal).
	links := bodyString(t, a.get(t, client, "/admin/links"))
	if !strings.Contains(links, ">Links<") {
		t.Errorf("conceal mode nav should still say 'Links', got: %s", links)
	}

	// Login page should now show Nextcloud branding.
	jar2 := client.Jar
	anon := &http.Client{Jar: jar2}
	login := bodyString(t, a.get(t, anon, "/login"))
	if !strings.Contains(login, "Nextcloud") {
		t.Errorf("conceal mode ON: /login should mention Nextcloud, got: %s", login)
	}

	// Toggle back off.
	a.postForm(t, client, "/admin/settings/conceal", nil).Body.Close()
	settings = bodyString(t, a.get(t, client, "/admin/settings"))
	if !strings.Contains(settings, "Conceal mode is OFF") {
		t.Fatal("conceal mode did not turn back OFF")
	}
}

// TestGeoIPToggleSkipsLookupFields verifies that with GeoIP disabled,
// events still log (IP/device/timestamp), just without geo fields set —
// per the documented behavior, not a broken capture path.
func TestGeoIPToggleSkipsLookupFields(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	a.postForm(t, client, "/admin/settings/geoip", nil).Body.Close() // starts enabled by default per config; this disables it
	settings := bodyString(t, a.get(t, client, "/admin/settings"))
	if !strings.Contains(settings, "GeoIP is OFF") {
		t.Fatalf("expected GeoIP OFF after toggle, settings page: %s", settings)
	}

	a.createLink(t, client, url.Values{"type": {"redirect"}, "slug": {"geooff"}, "destination": {"https://example.com"}}).Body.Close()
	anon := noRedirectClient(&http.Client{Jar: client.Jar})
	a.get(t, anon, "/s/geooff").Body.Close()

	events := a.eventsJSON(t, client, "")
	for _, e := range events {
		if e["Type"] == "click" {
			if country, _ := e["Country"].(string); country != "" {
				t.Errorf("GeoIP disabled but event has Country=%q, want empty", country)
			}
			if ip, _ := e["IP"].(string); ip == "" {
				t.Error("GeoIP disabled: event IP should still be logged, got empty")
			}
			return
		}
	}
	t.Fatal("no click event found for /s/geooff")
}

// TestEventsCSVDefusesFormulaInjection is the regression test for a real
// CSV/formula-injection gap found during a security review: a visitor's
// User-Agent (attacker-controlled, always) was written into the CSV export
// completely unescaped. A value starting with =/+/-/@ is interpreted as a
// formula by Excel/LibreOffice/Sheets when the admin opens the export,
// which can exfiltrate data or run commands on the admin's own machine.
func TestEventsCSVDefusesFormulaInjection(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)
	a.createLink(t, client, url.Values{"type": {"redirect"}, "slug": {"csvtest"}, "destination": {"https://example.com"}}).Body.Close()

	req, err := http.NewRequest(http.MethodGet, a.srv.URL+"/s/csvtest", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("User-Agent", `=cmd|'/c calc'!A1`)
	nr := noRedirectClient(client)
	resp, err := nr.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()

	csvResp := a.get(t, client, "/admin/events.csv")
	defer csvResp.Body.Close()
	body := bodyString(t, csvResp)

	if strings.Contains(body, "\n=cmd") || strings.Contains(body, ",=cmd") {
		t.Fatalf("CSV export contains an unescaped formula-injection payload: %s", body)
	}
	if !strings.Contains(body, "cmd|'/c calc'!A1") {
		t.Fatalf("CSV export lost the user-agent value entirely (should be defused, not dropped): %s", body)
	}
}

// TestEventsSearchAndDelete exercises the admin events log's search filter
// and per-event / bulk delete.
func TestEventsSearchAndDelete(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	a.createLink(t, client, url.Values{"type": {"redirect"}, "slug": {"ev1"}, "destination": {"https://example.com"}}).Body.Close()
	a.createLink(t, client, url.Values{"type": {"redirect"}, "slug": {"ev2"}, "destination": {"https://example.com"}}).Body.Close()

	anon := noRedirectClient(&http.Client{Jar: client.Jar})
	a.get(t, anon, "/s/ev1").Body.Close()
	a.get(t, anon, "/s/ev2").Body.Close()

	all := a.eventsJSON(t, client, "")
	if len(all) != 2 {
		t.Fatalf("expected 2 events total, got %d", len(all))
	}

	filtered := a.eventsJSON(t, client, "ev1")
	if len(filtered) != 1 {
		t.Fatalf("search for 'ev1' returned %d events, want 1", len(filtered))
	}

	var id float64
	for _, e := range all {
		if id == 0 {
			id = e["ID"].(float64)
		}
	}
	nr := noRedirectClient(client)
	resp := a.postForm(t, nr, "/admin/api/events/delete", url.Values{"ids": {itoa(int64(id))}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete event: got %d, want 200", resp.StatusCode)
	}

	remaining := a.eventsJSON(t, client, "")
	if len(remaining) != 1 {
		t.Fatalf("after deleting 1 of 2 events, got %d remaining, want 1", len(remaining))
	}
}
