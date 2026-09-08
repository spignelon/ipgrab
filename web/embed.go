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
