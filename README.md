# potly

<p align="center">
  <img src="potly-thumbnail.jpg" alt="potly: a single-binary URL shortener with a built-in public tunnel" width="360">
</p>

potly is a self-hostable URL shortener in a single binary. One command serves it locally and publishes a public HTTPS URL, with no deploy step. Custom slugs give links like `potly.thumbgo.kr/portfolio`.

> **What is Portal?** A safe, free, open-source self-hosting tunnel: [github.com/gosuda/portal-tunnel](https://github.com/gosuda/portal-tunnel).

## How to use

### 1. Use it from an agent

Prompt your agent, e.g.:

```text
Look at https://github.com/gosuda/potly, deploy it, and send URLs as short links.
```

### 2. Use it from the marketplace

Install the Claude Code plugin:

```
/plugin marketplace add gosuda/potly
/plugin install potly@potly
```

### 3. Run it yourself

Requires [Go](https://go.dev) 1.27 or newer.

```sh
git clone https://github.com/gosuda/potly
cd potly
go build -o potly .
./potly -portal     # public URLs appear on stderr; Ctrl-C tears the tunnel down
```

Portal mode is the default way to run. For local-only use, drop the flag: `./potly` serves `http://localhost:8000`. Portal picks several relays automatically, so you get one public URL per relay.

`-name` sets your subdomain, and `-relays` pins a specific Portal domain when you want one:

```sh
./potly -portal -name myapp -relays https://gosunuts.xyz    # https://myapp.gosunuts.xyz
```

No Go toolchain? Run the published image instead: `docker run -p 8000:8000 -v potly:/data ghcr.io/gosuda/potly -portal`.

## License

[MIT](LICENSE).
