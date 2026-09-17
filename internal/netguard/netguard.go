// Package netguard provides an SSRF-guarded HTTP transport shared by every
// package in this app that fetches an operator-supplied URL on the
// server's behalf (the live-proxy clone engine, and the webhook
// notifier) — both let an admin configure an arbitrary destination that
// this server then makes requests to, which is exactly the shape of an
// SSRF vector if that destination (or one it redirects to) turns out to
// resolve to an internal address.
//
// A naive guard that does a DNS lookup, checks the result, and THEN makes a
// separate request (which re-resolves DNS on its own) has a TOCTOU gap: an
// attacker-controlled domain with a very short DNS TTL can resolve to a
// public IP for the check and then to a loopback/private address by the
// time the real connection happens ("DNS rebinding"). Transport pins the
// connection to the specific IP address it just validated — the same
// hostname is never resolved twice — closing that gap.
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// AllowPrivateForTesting disables the private/loopback-address restriction
// when true. It exists ONLY so the end-to-end test suite can point guarded
// clients at a local httptest server without depending on the real
// internet — never set outside tests.
var AllowPrivateForTesting bool

// GuardScheme rejects any URL that isn't plain http/https, or has no host —
// a cheap pre-check callers can use before even attempting a fetch.
func GuardScheme(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("missing host")
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

var dialer = &net.Dialer{Timeout: 10 * time.Second}

// dialContext resolves addr's host itself, validates every resolved address
// is public, and dials the specific validated IP directly — the hostname is
// never handed to a second, independent resolution step, so there's no
// window for its DNS record to change between check and connect. The
// original hostname still flows through for TLS SNI/certificate
// verification, since only the dial target (not the request's URL) changes.
func dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("dns lookup failed: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses found for %s", host)
	}
	if !AllowPrivateForTesting {
		for _, ip := range ips {
			if !isPublic(ip) {
				return nil, fmt.Errorf("refusing to connect to non-public address %s", ip)
			}
		}
	}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

// Transport returns an *http.Transport whose every connection is guarded by
// dialContext — safe to assign directly to an http.Client's Transport
// field. Redirects are the caller's responsibility to cap (via
// http.Client.CheckRedirect); each redirect hop is still a normal request
// through this same transport, so it's guarded identically regardless.
func Transport() *http.Transport {
	return &http.Transport{DialContext: dialContext}
}
