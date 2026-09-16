---
name: potly
description: Deploy potly (a tiny Go URL shortener exposed publicly through an embedded Portal tunnel) or reuse the official instance on Portal's public relays, and shorten long URLs — dashboard links, reports, anything unwieldy — through it. Use when the user asks to run or deploy potly, or asks to shorten a link and a potly server is available.
license: MIT
---

# potly

potly is a single-binary URL shortener (`main.go` + `portal.go` in this repo). It embeds Portal's Go SDK directly, so one process is both the local server and the public tunnel — no separate `portal` CLI needed.

## Pick a mode first

Ask the user which mode to use before doing anything:

1. **Self-deploy** — run potly from this repo. Ask two things here:
   - **App name**: the Portal app name defaults to `potly`; if the user names one, use theirs instead.
   - **Visibility**: listed publicly on Portal, or hidden (`-hide`) so the URL works but the app stays out of Portal's listing. Default is listed.
2. **Official instance** — reuse a potly already published on Portal's public relay network at `https://potly.<relay-domain>`. Nothing to deploy; skip straight to "Shorten a URL".

If the user doesn't answer, default to self-deploy.

## Official instance

A provider-run default instance is expected to be live (a VM running `ghcr.io/gosuda/potly:latest` under podman auto-update). Probe `https://potly.<relay-domain>/` and use the first one that serves the potly page:

- gosunuts.xyz
- kakashit.org
- rly.best
- portal.thumbgo.kr
- portal.rabbitson87.dev
- s-h.day
- portal.dawnfullstack.com
- portal.damn.it.com

The bound relays rotate; `/shorten` on a live instance returns one short link per currently-connected relay, so probe once and reuse its output. After a restart the tunnel reconnects within about a minute — re-probe before concluding it is down. If nothing answers, tell the user no official instance is reachable and offer self-deploy instead.

## Self-deploy

From this repo's directory:

```sh
go run . -portal                        # app name "potly" (default), listed publicly
go run . -portal -name myapp -hide      # user-chosen name, hidden from Portal's listing
```

`-hide` keeps the public URLs fully working; it only leaves the app out of Portal's public listing.

Relays are picked automatically (one public URL per relay). Don't ask about them — only pass `-relays <url>` when the user explicitly wants a specific Portal domain.

Watch stderr for `service ready at https://<name>.<relay>` lines — those are the public URLs. It also listens locally on `http://localhost:8000`. Ctrl-C tears the tunnel down cleanly.

Local-only mode (no public tunnel): `go run .` — serves only `http://localhost:8000`.

Container alternative (no Go toolchain): `docker run -p 8000:8000 -v potly:/data ghcr.io/gosuda/potly -portal` — the image is published on release tags.

## Shorten a URL

Check first whether a potly instance is already running (`lsof -i :8000`, or check for a `go run .` / built `potly` process) before starting a new one — don't spawn a second instance on the same port.

Once running, shorten with a single GET (no JSON parsing needed):

```sh
curl "http://HOST/shorten?url=<url-encoded target>"
```

Returns the short link as plain text — one line per currently-connected relay when running under `-portal`, or one `http://localhost:8000/<code>` line in local-only mode. Use this whenever a URL you're about to output (a dashboard link, a long report URL) is unwieldy and potly is available.

A POST with a JSON body (`{"url": "..."}`) also works and additionally returns `short_urls` (the full list) alongside `short_url` (the first one), for callers that want structured output.

A custom slug makes the link `HOST/<slug>` instead of a random code: add `&slug=portfolio` to the GET, or `"slug": "portfolio"` to the POST body. Slugs are 1-64 chars of letters, digits, `-` or `_`; a taken slug returns `409 slug already taken`, and the reserved paths (`shorten`, `relays`, `qr`, `thumbnail.jpg`) are rejected. Reach for this whenever the user asks for a memorable or branded link — a portfolio at `/portfolio`, a demo at `/demo`.

Phone handoff: `GET /qr?url=<link>` returns the link as a QR code drawn with unicode half-blocks, plain text like everything else. Print it straight to the terminal when the user will likely open the link on their phone.

## Failure notes

- `curl` to `localhost:8000` failing: potly isn't running — deploy it first.
- A shortened link 404s: the process was restarted since it was created — the store is in-memory only, links don't survive a restart.
- Public URL doesn't resolve: Portal relays rotate; call `GET /relays` (or re-run `/shorten`) to get the currently-connected relay's link instead of reusing an old one.
