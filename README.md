# Netra

A self-hosted IP address, device, and location intelligence toolkit for **authorized**
security assessments — OSINT gathering, phishing-simulation exercises, and physical/remote
audit engagements. Single admin account, single Docker container, SQLite storage, no external
accounts required.

Lightweight: Docker image is **~23 MB**, container idles at **~24 MB RAM**.

> Same category as Grabify, IPLogger, GoPhish, and Canarytokens — for people who already run
> these kinds of engagements professionally and want a self-hosted alternative they control.

📖 **[See the Wiki](../../wiki) for architecture details, the live-proxy clone engine internals,
conceal mode, known limitations, and deployment guides.**

---

## ⚠️ Disclaimer — read before you deploy this

**Netra is intended for lawful, authorized use only** — penetration tests, red-team
engagements, phishing-awareness training, and security audits you are contractually and
legally authorized to perform, typically with a signed scope-of-work / rules-of-engagement
document and, where applicable, informed consent from the people being tested.

Do **not** use this tool to track, locate, or profile anyone without authorization; send
tracking links/pixels outside an approved engagement; collect location or device data covered
by GDPR/CCPA without a valid legal basis; or harass, stalk, or dox anyone.

You are solely responsible for how you use this software and for complying with the laws that
apply to you, your organization, and your targets. The authors accept no liability for misuse.
**When in doubt, don't send it — get it in writing first.**

---

## Quick start (Docker)

```bash
git clone https://github.com/spignelon/netra.git && cd netra
cp .env.example .env
# edit .env — at minimum set BASE_URL and SESSION_SECRET:
#   openssl rand -hex 32   (paste the output as SESSION_SECRET)

docker compose up -d --build
```

Visit `BASE_URL` (e.g. `http://localhost:8080`) — you'll land on `/setup` to create the one
admin account, then log in at `/login`.

Data persists in the `netra_data` Docker volume across `docker compose down`/`up`. See the
Wiki's [Quick Start](../../wiki/Quick-Start) page for running without Docker, behind a reverse
proxy, and the full environment-variable reference.

## What it does

Four kinds of "capture" link, generated from one dashboard:

| Type | Route | Use case |
|---|---|---|
| **Redirect / shortener** | `/s/{slug}` | A normal-looking short link. Logs the visitor, then redirects to a real URL you choose. |
| **Tracking pixel** | `/i/{slug}.png` | A 1×1 image (or your own upload) to embed in an email/document. Logs when it loads. |
| **GPS decoy page** | `/g/{slug}` | A themed page that requests browser geolocation — or live-proxies a real page instead, with the same capture script injected. |
| **Cloned / preview link** | `/p/{slug}` | **Live-proxies** a real destination — served directly, title/description/OG tags intact, so it unfurls identically to the original in chat apps. |

Every visit captures (where available): IP, geolocated country/region/city and ISP/org/ASN,
device/OS/browser, referer, and a few JS-side signals (timezone, screen size, platform). GPS
events additionally store precise coordinates from the browser's Geolocation API.

## Key features

- **Link expiration** — expiry date and/or max-click count, with a one-time "expired" webhook.
- **QR codes**, **CSV export**, and a **searchable global event log** alongside each link's own.
- **Webhook notifications** via self-hosted [ntfy](https://ntfy.sh) or [Gotify](https://gotify.net),
  with a separate high-priority alert for GPS captures and per-link channel overrides.
- **GeoIP toggle** — turn off outbound `ip-api.com` lookups entirely; events still log
  IP/device/timestamp.
- **Conceal mode** — disguises the admin login/dashboard as a self-hosted Nextcloud instance.
  Details on the [Conceal Mode](../../wiki/Conceal-Mode) wiki page.
- **Live-proxy clone engine** — a Service Worker reroutes the browser's own resource fetches
  through this server at the network layer, so cloned pages keep working (including
  React/Next.js-hydrated sites) without ever leaving your domain. Deep dive on the
  [Live-Proxy Clone Engine](../../wiki/Live-Proxy-Clone-Engine) wiki page.
- Single admin account (bcrypt + signed sessions + CSRF), dark-mode-aware dashboard, Leaflet
  map on each link's detail page.

## Architecture, at a glance

Go 1.26 + stdlib `net/http`, no web framework. SQLite via the pure-Go `modernc.org/sqlite`
driver (no cgo). Server-rendered `html/template` + vanilla JS frontend, Chart.js + Leaflet
vendored locally. Everything (templates, static assets) is embedded into one binary via
`//go:embed` — the Docker image is a single self-contained executable.

Full breakdown — package layout, the clone engine's Service Worker mechanism, the SSRF guard,
and known limitations — is in the [Wiki](../../wiki).

## License

Licensed under the **GNU General Public License v3.0** — see [`LICENSE`](LICENSE). Free to
use, modify, and self-host, including commercially, provided derivative works you distribute
stay under GPLv3 too. No warranty. Independent of the usage disclaimer above — both apply.
