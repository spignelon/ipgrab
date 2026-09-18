package e2e

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// createLink is a shared helper: submits the admin "create link" form and
// returns the numeric id the server redirected to (?created=1 for clone,
// or a redirect to /admin/links for the other three types, in which case
// this returns 0 and callers should look the link up by slug instead).
func (a *testApp) createLink(t *testing.T, client *http.Client, form url.Values) *http.Response {
	t.Helper()
	nr := noRedirectClient(client)
	return a.postForm(t, nr, "/admin/links", form)
}

func TestCreateEachLinkType(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	cases := []struct {
		name string
		form url.Values
	}{
		{"redirect", url.Values{"type": {"redirect"}, "slug": {"redir1"}, "destination": {"https://example.com/dest"}}},
		{"pixel", url.Values{"type": {"pixel"}, "slug": {"pix1"}}},
		{"gps", url.Values{"type": {"gps"}, "slug": {"gps1"}, "theme": {"cats"}}},
		{"clone", url.Values{"type": {"clone"}, "slug": {"clone1"}, "destination": {"https://example.com/"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := a.createLink(t, client, tc.form)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusSeeOther {
				t.Fatalf("create %s link: got %d, want 303", tc.name, resp.StatusCode)
			}
			loc := resp.Header.Get("Location")
			if !strings.HasPrefix(loc, "/admin/links") {
				t.Fatalf("create %s link: unexpected redirect %q", tc.name, loc)
			}
		})
	}

	// All four should now show up in the list.
	list := a.get(t, client, "/admin/links")
	body := bodyString(t, list)
	for _, slug := range []string{"redir1", "pix1", "gps1", "clone1"} {
		if !strings.Contains(body, slug) {
			t.Errorf("links list missing slug %q", slug)
		}
	}
}

func TestInvalidSlugRejected(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	resp := a.createLink(t, client, url.Values{
		"type": {"redirect"}, "slug": {"has spaces!"}, "destination": {"https://example.com"},
	})
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Fatalf("invalid slug: got redirect %q, want an err= query param", loc)
	}
}

func TestDuplicateSlugRejected(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	form := url.Values{"type": {"redirect"}, "slug": {"dupe"}, "destination": {"https://example.com"}}
	a.createLink(t, client, form).Body.Close()

	resp := a.createLink(t, client, url.Values{"type": {"redirect"}, "slug": {"dupe"}, "destination": {"https://example.com/2"}})
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "already+taken") && !strings.Contains(loc, "already%20taken") {
		t.Fatalf("duplicate slug: got redirect %q, want an 'already taken' error", loc)
	}
}

func TestToggleAndDeleteLink(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	a.createLink(t, client, url.Values{"type": {"redirect"}, "slug": {"togglable"}, "destination": {"https://example.com"}}).Body.Close()
	id := a.linkIDBySlug(t, client, "togglable")

	// Disabling it should make the public route 404.
	a.postForm(t, client, "/admin/links/"+itoa(id)+"/toggle", nil).Body.Close()
	pub := a.get(t, client, "/s/togglable")
	defer pub.Body.Close()
	if pub.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled link /s/togglable: got %d, want 404", pub.StatusCode)
	}

	// Re-enable, then delete outright.
	a.postForm(t, client, "/admin/links/"+itoa(id)+"/toggle", nil).Body.Close()
	a.postForm(t, client, "/admin/links/"+itoa(id)+"/delete", nil).Body.Close()

	list := a.get(t, client, "/admin/links")
	body := bodyString(t, list)
	if strings.Contains(body, "togglable") {
		t.Fatal("deleted link still appears in /admin/links")
	}
}

func TestBulkDeleteLinks(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	for _, slug := range []string{"bulk1", "bulk2", "bulk3"} {
		a.createLink(t, client, url.Values{"type": {"redirect"}, "slug": {slug}, "destination": {"https://example.com"}}).Body.Close()
	}
	id1 := a.linkIDBySlug(t, client, "bulk1")
	id2 := a.linkIDBySlug(t, client, "bulk2")

	nr := noRedirectClient(client)
	resp := a.postForm(t, nr, "/admin/links/delete", url.Values{"ids": {itoa(id1), itoa(id2)}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("bulk delete: got %d, want 303", resp.StatusCode)
	}

	body := bodyString(t, a.get(t, client, "/admin/links"))
	if strings.Contains(body, "bulk1") || strings.Contains(body, "bulk2") {
		t.Fatal("bulk-deleted links still appear in /admin/links")
	}
	if !strings.Contains(body, "bulk3") {
		t.Fatal("bulk3 (not selected for deletion) is missing — bulk delete removed too much")
	}
}

// TestLinksAPILiveCounters is the regression test for the Links page's
// auto-refresh: /admin/api/links must reflect a new hit's event count and
// active state without requiring a page reload, matching what /admin/links
// itself would show.
func TestLinksAPILiveCounters(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	a.createLink(t, client, url.Values{"type": {"redirect"}, "slug": {"apitest"}, "destination": {"https://example.com"}}).Body.Close()
	id := a.linkIDBySlug(t, client, "apitest")

	resp := a.get(t, client, "/admin/api/links")
	defer resp.Body.Close()
	var rows []struct {
		ID         int64  `json:"id"`
		EventCount int    `json:"event_count"`
		LastEvent  string `json:"last_event"`
		Active     bool   `json:"active"`
		Expired    bool   `json:"expired"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode /admin/api/links: %v", err)
	}
	var before *struct {
		ID         int64  `json:"id"`
		EventCount int    `json:"event_count"`
		LastEvent  string `json:"last_event"`
		Active     bool   `json:"active"`
		Expired    bool   `json:"expired"`
	}
	for i := range rows {
		if rows[i].ID == id {
			before = &rows[i]
		}
	}
	if before == nil {
		t.Fatalf("link id %d missing from /admin/api/links response", id)
	}
	if before.EventCount != 0 || before.LastEvent != "" || !before.Active {
		t.Fatalf("unexpected initial state for a fresh link: %+v", before)
	}

	anon := noRedirectClient(&http.Client{Jar: client.Jar})
	a.get(t, anon, "/s/apitest").Body.Close()

	resp2 := a.get(t, client, "/admin/api/links")
	defer resp2.Body.Close()
	rows = nil
	if err := json.NewDecoder(resp2.Body).Decode(&rows); err != nil {
		t.Fatalf("decode /admin/api/links after hit: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.ID != id {
			continue
		}
		found = true
		if row.EventCount != 1 {
			t.Errorf("expected event_count=1 after one hit, got %d", row.EventCount)
		}
		if row.LastEvent == "" {
			t.Error("expected last_event to be set after a hit, got empty")
		}
	}
	if !found {
		t.Fatalf("link id %d missing from /admin/api/links after hit", id)
	}
}

func TestQRCodeIsValidPNG(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	a.createLink(t, client, url.Values{"type": {"redirect"}, "slug": {"qrtest"}, "destination": {"https://example.com"}}).Body.Close()
	id := a.linkIDBySlug(t, client, "qrtest")

	resp := a.get(t, client, "/admin/links/"+itoa(id)+"/qr.png")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("qr.png: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("qr.png content-type = %q, want image/png", ct)
	}
	body := bodyString(t, resp)
	img, err := png.Decode(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("qr.png did not decode as a valid PNG: %v", err)
	}
	if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		t.Fatal("qr.png decoded but has zero dimensions")
	}
}

func TestMaxClicksExpiry(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	a.createLink(t, client, url.Values{
		"type": {"redirect"}, "slug": {"limited"}, "destination": {"https://example.com"}, "max_clicks": {"2"},
	}).Body.Close()

	anon := noRedirectClient(&http.Client{Jar: client.Jar})
	for i := 0; i < 2; i++ {
		resp := a.get(t, anon, "/s/limited")
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("click %d: got %d, want 302", i+1, resp.StatusCode)
		}
	}
	// Third click should now be past max_clicks and 404.
	resp := a.get(t, anon, "/s/limited")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("click past max_clicks: got %d, want 404", resp.StatusCode)
	}
}

// linkIDBySlug looks up a link's numeric id by scanning the admin links
// list for the <tr> whose share-URL cell contains the slug (an untitled
// link's own display name is "#<id>", not its slug, so the row's URL
// input — which always embeds the slug — is what identifies it), then
// pulling the id out of that same row's /admin/links/{id} detail href.
// Avoids reaching into the DB directly so this stays a true black-box test.
func (a *testApp) linkIDBySlug(t *testing.T, client *http.Client, slug string) int64 {
	t.Helper()
	body := bodyString(t, a.get(t, client, "/admin/links"))
	// Split on "<tr" (not "<tr>") so this stays correct regardless of
	// attributes on the row tag (e.g. the data-id added for auto-refresh).
	for _, row := range strings.Split(body, "<tr") {
		if !strings.Contains(row, "/"+slug+"\"") && !strings.Contains(row, "/"+slug+"<") {
			continue
		}
		href := `/admin/links/`
		pos := strings.Index(row, href)
		if pos == -1 {
			continue
		}
		rest := row[pos+len(href):]
		end := strings.IndexByte(rest, '"')
		if end == -1 {
			continue
		}
		id, err := parseInt(rest[:end])
		if err == nil {
			return id
		}
	}
	t.Fatalf("could not find a link row for slug %q in /admin/links", slug)
	return 0
}
