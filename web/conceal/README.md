# Conceal mode assets

These three files are the **real Nextcloud brand assets**, fetched directly from an official
`nextcloud:latest` Docker image (a fresh, fully set-up local instance) rather than recreated
by hand:

- `logo.svg` — `/core/img/logo/logo.svg`
- `favicon.svg` — `/core/img/favicon.svg`
- `background.webp` — `/apps/theming/img/background/jo-myoung-hee-fluid.webp` (the default
  login screen background)

They ship as part of the [Nextcloud server](https://github.com/nextcloud/server) source tree
under its AGPLv3 license. The exact colors, spacing, and structure used in
`web/templates/login.html`'s conceal-mode branch were likewise taken from the real rendered
login page (captured with a headless browser against that same local instance), not
approximated — see the "Conceal mode" section of the top-level README for how this is used
and its limitations.
