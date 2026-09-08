// Package web embeds the HTML templates and static assets so the compiled
// binary is fully self-contained.
package web

import "embed"

// Templates holds all HTML templates.
//
//go:embed templates/*.html
var Templates embed.FS

// Static holds CSS/JS/vendor assets served under /static/.
//
//go:embed static/*
var Static embed.FS

// Conceal-mode assets — the real Nextcloud logo/favicon/login-background,
// fetched from an official `nextcloud` Docker image (see
// web/conceal/README.md). Deliberately embedded from OUTSIDE web/static/ and
// served (in public.go) at paths that mirror Nextcloud's own real asset
// URLs, not under /static/conceal/ — a URL literally containing the word
// "conceal" would give away the disguise to anyone viewing page source.
//
//go:embed conceal/logo.svg
var ConcealLogo []byte

//go:embed conceal/favicon.svg
var ConcealFavicon []byte

//go:embed conceal/background.webp
var ConcealBackground []byte
