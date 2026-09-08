# IPGrab

A self-hosted IP address, device, and location intelligence toolkit for **authorized**
security assessments — OSINT gathering, phishing-simulation exercises, and physical/remote
audit engagements. Single admin, single Docker container, SQLite storage, no external
accounts required.

> **This is the same category of tool as Grabify, IPLogger, GoPhish, and Canarytokens.**
> It is built for people who already run these kinds of engagements professionally and need
> a self-hosted alternative they fully control.

---

## ⚠️ Disclaimer — read before you deploy this

**IPGrab is intended for lawful, authorized use only** — penetration tests, red-team
engagements, phishing-awareness training, and security audits that you are contractually
and legally authorized to perform, typically with a signed scope-of-work / rules-of-engagement
document and, where applicable, the informed consent of the individuals being tested.

Do **not** use this tool to:
- track, locate, or profile a person without authorization or a lawful basis;
- send tracking links or pixels to people outside an approved engagement;
- collect location or device data covered by GDPR, CCPA, or similar laws without a valid
  legal basis and, where required, consent;
- harass, stalk, dox, or otherwise cause harm to any individual.

You are solely responsible for how you use this software and for complying with the laws and
regulations that apply to you, your organization, and your targets. The authors accept no
liability for misuse. **When in doubt, don't send it — get it in writing first.**

---

## What it does

IPGrab lets you generate four kinds of "capture" links from one dashboard:

| Type | Route | Use case |
|---|---|---|
| **Redirect / shortener** | `/s/{slug}` | A normal-looking short link. Logs the visitor, then 302-redirects to a real URL you choose. |
| **Tracking pixel** | `/i/{slug}.png` | A 1×1 transparent image (or your own uploaded image) to embed in an HTML email or document. Logs when it's loaded. |
| **GPS decoy page** | `/g/{slug}` | A themed landing page (e.g. "cute cat pictures") that requests the visitor's browser geolocation. If they accept, precise GPS coordinates are logged. |
| **Cloned / preview link** | `/p/{slug}` | Renders Open Graph tags so the link unfurls with a rich preview (title/description/image) in chat apps, then logs a click-through when the visitor continues. |

Every visit captures (where available): IP address, geolocated country/region/city and
ISP/org/ASN (via ip-api.com), User-Agent-derived device/OS/browser, referer, Accept-Language,
and a handful of JS-side signals (timezone, screen size, platform). GPS events additionally
store precise latitude/longitude/accuracy from the browser's Geolocation API.

## Dashboard features

- **Overview** — total links, total events, GPS captures, unique IPs, and graphs (events over
  time, top countries, device types, browsers).
- **Links** — create/list/toggle/delete every link type from one page; each link shows its
  share URL, event count, and last-activity time.
- **Link detail** — full event log for that link, a Leaflet map plotting every geolocated /
  GPS point, and a CSV export scoped to that link.
- **CSV export** — full event log, or scoped to one link, from `/admin/events.csv`.
- Single admin account, created via a first-run `/setup` page; bcrypt-hashed password,
  signed session cookies, CSRF protection on all state-changing admin actions.
- Dashboard follows your OS/browser dark-mode setting automatically (`prefers-color-scheme`) —
  no toggle needed.

## Known limitations (by design, not bugs)

- **Email tracking pixels are increasingly unreliable.** Gmail proxies remote images through
  Google's own servers, and Apple Mail Privacy Protection pre-fetches every image regardless
  of whether the user opens the email — so a pixel often logs the *mail provider's* IP, not
  the recipient's. Redirect and clone links are far more reliable signal sources.
- **GPS location requires the visitor to accept the browser permission prompt.** This is a
  hard browser/OS requirement and cannot be bypassed — if they decline, you still get IP-based
  geolocation and device fingerprinting, just not precise GPS coordinates.
- **ip-api.com** (the free geolocation API this project uses) is HTTP-only and rate-limited to
  ~45 requests/minute. Results are cached in memory for 6 hours per IP to stay under that limit.
- Not built and will not be added: fake login/credential-harvesting pages, sender/domain
  spoofing, or anti-spam/AV evasion tooling. This project stays in the "logging link" lane.

## Quick start (Docker)

```bash
git clone https://github.com/spignelon/ipgrab.git && cd ipgrab
cp .env.example .env
# edit .env — at minimum set BASE_URL and SESSION_SECRET:
#   openssl rand -hex 32   (paste the output as SESSION_SECRET)

docker compose up -d --build
```

Visit `BASE_URL` (e.g. `http://localhost:8080`) — you'll land on `/setup` to create the one
admin account. After that, log in at `/login` and you're on the dashboard.

Data (the SQLite database and any uploaded pixel images) persists in the `ipgrab_data` Docker
named volume, so `docker compose down` / `up` won't lose anything. If you'd rather have the
database file directly accessible on the host, edit `docker-compose.yml` to bind-mount a local
folder instead (`./data:/data`) — just make sure that folder is writable by UID 100 (the
container's non-root `ipgrab` user), e.g. `mkdir -p data && chown 100:101 data`.

## Running behind a reverse proxy / on the public internet

- Put a real reverse proxy (nginx, Caddy, Traefik) in front and terminate TLS there.
- Set `BASE_URL` to your real HTTPS domain so generated links are correct.
- Set `TRUST_PROXY=true` **only if** that proxy is the sole way to reach the container —
  otherwise visitors can spoof `X-Forwarded-For` and fake their own IP in your logs.
- Set `COOKIE_SECURE=true` once you're serving over HTTPS.

## Running without Docker

```bash
go build -o ipgrab ./cmd/ipgrab
SESSION_SECRET=$(openssl rand -hex 32) BASE_URL=http://localhost:8080 ./ipgrab
```

Requires Go 1.26+. The sqlite driver is pure Go (`modernc.org/sqlite`), so no cgo/gcc is
needed.

## Configuration reference

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | TCP port to listen on. |
| `BASE_URL` | `http://localhost:$PORT` | Public base URL used to render shareable links. |
| `DATA_DIR` | `./data` | Directory for the SQLite DB and uploaded pixel images. |
| `SESSION_SECRET` | *(random, ephemeral)* | Secret for signing sessions — **set this in production**, or you'll be logged out on every restart. |
| `TRUST_PROXY` | `false` | Honour `X-Forwarded-For`/`X-Real-IP`. Only enable behind a trusted proxy. |
| `COOKIE_SECURE` | `false` | Mark the session cookie `Secure`. Enable once serving over HTTPS. |

## Architecture

- **Backend:** Go 1.26, standard-library `net/http` (pattern-based `ServeMux`), no web
  framework.
- **Database:** SQLite via the pure-Go `modernc.org/sqlite` driver — no cgo, single static
  binary.
- **Frontend:** server-rendered `html/template` pages + vanilla JS, Chart.js for graphs and
  Leaflet for the location map, both vendored locally (`web/static/`) — no CDN dependency at
  runtime except OpenStreetMap map tiles on the link-detail page.
- **GeoIP enrichment:** [ip-api.com](https://ip-api.com) free JSON API, in-memory cached.
- Everything (templates + static assets) is embedded into the binary with `//go:embed`, so
  the Docker image is a single self-contained executable plus its SQLite data volume.

## License

Use, modify, and self-host freely for your own authorized security work. No warranty. See
the disclaimer above.
