# mailx-shim

An API shim that lets [Bitwarden](https://bitwarden.com) (and other [addy.io](https://addy.io)-compatible clients) create email aliases via the [IVPN Mailx](https://www.ivpn.net/blog/mailx-beta-audited-open-source-email-aliasing/) API.

> **Disclaimer:** This project is not affiliated with, endorsed by, or associated with IVPN, Bitwarden, or addy.io. It is an independent, community-built API translation layer designed to work with IVPN's open-source [Mailx](https://github.com/ivpn/mailx) service through Bitwarden's existing addy.io integration.

## Why

Bitwarden can generate forwarded email aliases through services like addy.io and SimpleLogin. IVPN Mailx is a newer, audited, open-source email aliasing service — but its API isn't compatible with any of the providers Bitwarden supports.

mailx-shim translates between the two: Bitwarden speaks addy.io, the shim speaks Mailx.

## How it works

```
Bitwarden client                mailx-shim                     Mailx API
     │                               │                            │
     │  POST /api/v1/aliases         │                            │
     │  { "domain": "github.com" }   │                            │
     │ ─────────────────────────────>│                            │
     │                               │  POST /api/authenticate    │
     │                               │  { "access_key": "..." }   │
     │                               │ ──────────────────────────>│
     │                               │  { "token": "jwt..." }     │
     │                               │ <──────────────────────────│
     │                               │                            │
     │                               │  POST /api/alias           │
     │                               │  { "domain": "ambox.net",  │
     │                               │    "recipients": "you@..",  │
     │                               │    "description": "github.com",
     │                               │    "enabled": true }       │
     │                               │ ──────────────────────────>│
     │                               │  { alias: { name: "x@.." }}│
     │                               │ <──────────────────────────│
     │                               │                            │
     │  { "data": { "email":         │                            │
     │    "x@ambox.net" } }          │                            │
     │ <─────────────────────────────│                            │
```

The "Email domain" field you enter in Bitwarden becomes the alias description in Mailx, so you can identify which alias belongs to which service.

## Quick start

```bash
cp .env.example .env
# Edit .env with your Mailx credentials and a strong API key

docker compose up -d
```

The shim listens on port 8080. Put a reverse proxy (Caddy, nginx, Traefik) in front for TLS.

## Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `MAILX_ACCESS_KEY` | yes | | Your Mailx API access key (85 characters) |
| `MAILX_RECIPIENT` | yes | | Email address that aliases forward to |
| `MAILX_DOMAIN` | yes | | Mailx domain to create aliases under (e.g. `mailx.net`) |
| `BRIDGE_API_KEY` | yes | | Bearer token that clients must present |
| `MAILX_BASE_URL` | no | `https://api.mailx.net/v1` | Mailx API base URL |
| `LISTEN_ADDR` | no | `:8080` | Address and port to listen on |

## Bitwarden setup

1. Open Bitwarden settings (browser extension, desktop, or mobile)
2. Go to **Settings > Generator** (or the username generator when creating a new item)
3. Select **Forwarded email alias**
4. Select **addy.io** as the service
5. Set **Base URL** to your shim's URL (e.g. `https://mailx.example.com`)
6. Set **API Key** to your `BRIDGE_API_KEY` value
7. Set **Email domain** to the website you're creating an alias for (e.g. `github.com`)

When you generate an alias, Bitwarden sends the request to the shim, which creates the alias in Mailx and returns it.

## Building

```bash
# Binary
go build -o mailx-shim .

# Docker image
docker build -t mailx-shim .
```

## Running tests

```bash
go test -race -v ./...
```

## Security considerations

- **API key**: Use a strong random value (`openssl rand -base64 48 | tr -d '=/+\n'`). The shim uses constant-time comparison.
- **TLS**: The shim does not terminate TLS. Place it behind a reverse proxy.
- **Rate limiting**: Not built in. Configure at the reverse proxy layer if needed.
- **Request size**: POST bodies are limited to 1 KiB.
- **No state**: The shim stores no data on disk. The only state is the cached Mailx session token in memory.

## License

[AGPL-3.0](LICENSE)
