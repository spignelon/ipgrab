// Package uaparse turns a raw User-Agent string into device/os/browser labels.
package uaparse

import (
	"strings"

	ualib "github.com/mssola/user_agent"
)

// Parsed is a simplified breakdown of a User-Agent.
type Parsed struct {
	Device  string // "Mobile", "Tablet", "Bot", or "Desktop"
	OS      string // e.g. "Windows 10", "Android 13", "iOS 17"
	Browser string // e.g. "Chrome 120"
}

// Parse extracts device/os/browser from a User-Agent header.
func Parse(ua string) Parsed {
	if strings.TrimSpace(ua) == "" {
		return Parsed{}
	}
	u := ualib.New(ua)

	name, version := u.Browser()
	browser := strings.TrimSpace(name + " " + majorVersion(version))

	p := Parsed{
		OS:      u.OS(),
		Browser: browser,
		Device:  device(u),
	}
	return p
}

func device(u *ualib.UserAgent) string {
	switch {
	case u.Bot():
		return "Bot"
	case u.Mobile():
		// mssola marks tablets as mobile; distinguish iPad/tablet UAs.
		plat := strings.ToLower(u.Platform())
		if strings.Contains(plat, "ipad") || strings.Contains(u.OS(), "iPad") {
			return "Tablet"
		}
		return "Mobile"
	default:
		return "Desktop"
	}
}

// majorVersion keeps only the leading major component of a version string.
func majorVersion(v string) string {
	if v == "" {
		return ""
	}
	if i := strings.IndexByte(v, '.'); i > 0 {
		return v[:i]
	}
	return v
}
