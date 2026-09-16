---
name: potly
description: Deploy potly (a tiny Go URL shortener exposed publicly through an embedded Portal tunnel) and shorten long URLs — dashboard links, reports, anything unwieldy — through it. Use when the user asks to run or deploy potly, or asks to shorten a link and this repo's potly server is available.
license: MIT
---

# potly

potly is a single-binary URL shortener (`main.go` + `portal.go` in this repo). It embeds Portal's Go SDK directly, so one process is both the local server and the public tunnel — no separate `portal` CLI needed.

## Deploy

From this repo's directory:

```sh
go run . -portal
```

Watch stderr for `service ready at https://potly.<relay>` lines — those are the public URLs. It also listens locally on `http://localhost:8000`. Ctrl-C tears the tunnel down cleanly.

Local-only mode (no public tunnel): `go run .` — serves only `http://localhost:8000`.

## Shorten a URL

Check first whether a potly instance is already running (`lsof -i :8000`, or check for a `go run .` / built `potly` process) before starting a new one — don't spawn a second instance on the same port.

Once running, shorten with a single GET (no JSON parsing needed):

```sh
curl "http://HOST/shorten?url=<url-encoded target>"
```

Returns the short link as plain text — one line per currently-connected relay when running under `-portal`, or one `http://localhost:8000/<code>` line in local-only mode. Use this whenever a URL you're about to output (a dashboard link, a long report URL) is unwieldy and potly is available.

A POST with a JSON body (`{"url": "..."}`) also works and additionally returns `short_urls` (the full list) alongside `short_url` (the first one), for callers that want structured output.

## Failure notes

- `curl` to `localhost:8000` failing: potly isn't running — deploy it first.
- A shortened link 404s: the process was restarted since it was created — the store is in-memory only, links don't survive a restart.
- Public URL doesn't resolve: Portal relays rotate; call `GET /relays` (or re-run `/shorten`) to get the currently-connected relay's link instead of reusing an old one.
