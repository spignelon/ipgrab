package e2e

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
)

// TestSetupAndLogin covers the first-run flow: no admin exists yet, /setup
// creates one and logs them in, and a second visit to /setup redirects to
// /login instead of allowing a second admin to be created.
func TestSetupAndLogin(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	// Setup already redirected us into a session — /admin should now load.
	resp := a.get(t, client, "/admin")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/admin after setup: got %d, want 200", resp.StatusCode)
	}

	// A second /setup visit must bounce to /login, not allow re-claiming.
	nr := noRedirectClient(client)
	resp2 := a.get(t, nr, "/setup")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther || resp2.Header.Get("Location") != "/login" {
		t.Fatalf("second /setup: got %d Location=%q, want 303 to /login", resp2.StatusCode, resp2.Header.Get("Location"))
	}
}

// TestAdminRequiresAuth asserts every admin route redirects an unauthenticated
// client to /login rather than serving content.
func TestAdminRequiresAuth(t *testing.T) {
	a := newTestApp(t)
	// Claim the instance so /admin exists in a normal (post-setup) state,
	// but use a client with NO session cookie to hit it.
	a.authedClient(t)

	jar, _ := cookiejar.New(nil)
	anon := noRedirectClient(&http.Client{Jar: jar})

	for _, path := range []string{"/admin", "/admin/links", "/admin/events", "/admin/settings"} {
		resp := a.get(t, anon, path)
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
			t.Errorf("GET %s unauthenticated: got %d Location=%q, want 303 to /login", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
}

// TestLoginWrongPassword ensures a bad password is rejected and does not
// grant a session.
func TestLoginWrongPassword(t *testing.T) {
	a := newTestApp(t)
	a.authedClient(t) // claims the instance with password "password12345"

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.PostForm(a.srv.URL+"/login", url.Values{
		"username": {"admin"},
		"password": {"totally-wrong"},
	})
	if err != nil {
		t.Fatalf("login post: %v", err)
	}
	defer resp.Body.Close()

	if a.csrfFromJar(client) != "" {
		t.Fatal("wrong password: got a session/CSRF cookie, want none")
	}

	nr := noRedirectClient(client)
	admin := a.get(t, nr, "/admin")
	defer admin.Body.Close()
	if admin.StatusCode != http.StatusSeeOther {
		t.Fatalf("/admin after failed login: got %d, want redirect to /login", admin.StatusCode)
	}
}

// TestCSRFEnforced verifies a state-changing admin POST is rejected without
// a matching csrf_token, even from an authenticated session.
func TestCSRFEnforced(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	resp, err := client.PostForm(a.srv.URL+"/admin/links", url.Values{
		"type": {"redirect"},
		"slug": {"nocsrf"},
		// csrf_token deliberately omitted
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("create link without csrf_token: got %d, want 403", resp.StatusCode)
	}
}

// TestLogout confirms the session cookie stops working after logout.
func TestLogout(t *testing.T) {
	a := newTestApp(t)
	client := a.authedClient(t)

	resp := a.postForm(t, client, "/logout", nil)
	resp.Body.Close()

	nr := noRedirectClient(client)
	admin := a.get(t, nr, "/admin")
	defer admin.Body.Close()
	if admin.StatusCode != http.StatusSeeOther {
		t.Fatalf("/admin after logout: got %d, want redirect to /login", admin.StatusCode)
	}
}
