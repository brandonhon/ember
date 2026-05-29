# Security

Ember has been written with the assumption that it will be exposed to the public internet behind a reverse proxy. This page lists the defenses in place and the deployment expectations.

For vulnerability reporting, see [SECURITY.md](https://github.com/brandonhon/ember/blob/main/SECURITY.md). Don't open public issues for security problems.

## Authentication

- **Passwords**: argon2id (`time=3`, `memory=64 MiB`, `parallelism=2`, salt=16 bytes). Meets OWASP 2024 recommendations.
- **Sessions**: 64-hex-char random IDs signed via `gorilla/securecookie`. Server-side row in `sessions` table backs every cookie. Default lifetime 24 hours; override via `EMBER_SESSION_TTL` env var or **Settings → Sessions** (bounded 5 min – 90 days). Destroyed on logout, login (fixation defense), and on password change (both self-service and admin).
- **Cookies**: `HttpOnly`, `Secure`, `SameSite=Strict`, scoped to `/`.
- **Rate limiting**: per-IP token bucket on `POST /api/auth/login` and `POST /api/auth/passkey/*`. Burst 10, refill 10 tokens/minute (one every 6s).
- **Passkeys / WebAuthn**: optional second sign-in method (FIDO2). Credentials are bound to a relying-party ID derived from `EMBER_PUBLIC_URL`; ceremonies expire after 5 minutes; a stale `webauthn_sessions` row is reaped on a 15-minute cadence. Credentials never leave the device — only the public key is stored.

## Authorization

| Endpoint group | Required |
| --- | --- |
| `/healthz` | none |
| `POST /api/auth/login` | none (login is the bootstrap) |
| All other `/api/*` | session cookie |
| `POST /api/users`, `PATCH /api/users/{id}`, admin LLM, branding, DB, settings | `is_admin = 1` |
| `GET /api/admin/settings`, `PATCH /api/admin/settings`, `POST /api/admin/settings/email-test` | `is_admin = 1` |
| `/metrics` | `is_admin = 1` |
| `GET /api/users` | returns `{id, username}` projection for non-admins |

Every user-scoped store query carries `WHERE user_id = ?` so users can't read each other's feeds, shares, tags, or saved searches. Article tag endpoints additionally call `requireArticleAccess`, which confirms the user is subscribed to the article's feed before allowing tag mutations.

## CSRF

Double-submit pattern. A random `ember_csrf` cookie (8 random bytes hex-encoded, not HttpOnly) must be echoed in the `X-Ember-CSRF` header on every state-changing request (`POST`, `PATCH`, `DELETE`).

Two safe exceptions:

- `POST /api/auth/login` — bootstrap, no session cookie yet.
- Anonymous requests to `GET` endpoints — no session means nothing to forge.

The login bypass uses **exact path match**, not suffix match.

## SSRF protection

Every outbound URL fetch passes through `internal/urlcheck.Check`:

- Scheme allowlist: `http`, `https`. Everything else (`file`, `gopher`, `javascript`) is refused.
- Private-IP block: literal IPs and DNS resolutions inside RFC1918 (`10/8`, `172.16/12`, `192.168/16`), loopback (`127/8`, `::1`), link-local (`169.254/16` — also the AWS / GCP / Azure metadata endpoint — and `fe80::/10`), CGNAT (`100.64/10`), unspecified (`0.0.0.0/8`), and IPv6 ULA (`fc00::/7`) are refused.
- Redirect chains: feed fetcher rejects 30x to private addresses via `feed.RedirectGuard`.
- Opt-in bypass: `EMBER_ALLOW_PRIVATE_URLS=1` skips the IP check for homelabs that need LAN feeds. Scheme allowlist still applies.

Surfaces covered:

- `POST /api/feeds` (add feed)
- `POST /api/feeds/import` (OPML import — each `xmlUrl` is filtered)
- Poller readability enrichment (Lobsters / HN aggregator → external link)
- Feed fetcher redirects
- `POST /api/articles/{id}/extract` (on-demand Re-extract) — runs the same `urlcheck.Check` before fetching the article URL through readability.

The admin-only `POST /api/admin/settings/email-test` opens an SMTP TCP connection to the admin-supplied `host:port`. This is **by design**: the same connection happens every 5 minutes from the digest sender when SMTP is configured. Access is gated by `is_admin = 1` so a non-admin can't use it as a port-scan or relay-probe primitive. On failure the endpoint returns a **generic** message; the underlying SMTP/TLS/DNS error (which can carry server banners, internal hostnames, or AUTH fragments) is logged server-side only.

## SMTP transport

Outbound mail (digests + the test message) never sends credentials or message bodies in cleartext across the network:

- `EMBER_SMTP_STARTTLS=1` (default) **requires** STARTTLS — if the server doesn't advertise it (or a MitM strips it from the EHLO response), the send fails rather than downgrading to plaintext. The TLS handshake pins `MinVersion: TLS 1.2` and verifies the server certificate.
- `EMBER_SMTP_STARTTLS=0` (plain SMTP) is permitted **only** to a loopback host (`localhost` / `127.0.0.1` / `::1`) — a local relay or sidecar. Plain SMTP to any remote host is refused before the connection opens.

## Body limits

- `decodeJSON` wraps the body in `http.MaxBytesReader` capped at **1 MiB**.
- OPML import body capped at **8 MiB**.
- `/api/articles/read` (and other bulk endpoints) accept at most **1000** ids per request.

## Error responses

5xx responses always read `{"error": {"code": "internal", "message": "internal error"}}`. The actual error is logged server-side via `slog.Default().Error(...)`. No SQLite errors, file paths, or constraint details leak to clients.

Ollama upstream errors (502 from `/api/admin/llm/pull` etc.) return generic "Ollama refused the pull" messages; the upstream text goes to logs.

## Transport / proxy expectations

- TLS is terminated upstream by Caddy (or your proxy). Ember's `:8080` should never be reachable directly from the internet.
- `X-Real-IP` is **only** honored when the immediate peer (`r.RemoteAddr`) is loopback, Docker (`172.16/12`), or LAN (`10/8`, `192.168/16`). Spoof attempts from outside fail through to `RemoteAddr`.
- HSTS, `Permissions-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, COOP, CORP, and a locked-down CSP (`default-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'`) are set on every response.

## Database

- SQLite WAL with single-writer semantics. `MaxOpenConns=1` to avoid `SQLITE_BUSY` storms.
- `synchronous=NORMAL` (safe with WAL), `busy_timeout=5s`, 64 MiB cache, 256 MiB mmap.
- Backups via `VACUUM INTO` are safe to run live and produce a compacted snapshot.

## Secrets at rest

Admin-editable secrets — currently the SMTP password (`smtp_password` key in `app_settings`) — are stored **as plaintext** in the SQLite database. This matches the storage model when the same value is supplied via `EMBER_SMTP_PASSWORD` (env vars are also plaintext, just in `.env` rather than `ember.db`).

Protect the SQLite file at the filesystem layer:

- Docker compose mounts `ember-data:/data` (root-owned inside the container).
- Backups produced by `/api/admin/db/backup` inherit those permissions. Don't ship them to anywhere less trustworthy than the host.
- Database-encryption-at-rest (SQLCipher) is not currently wired; if you need it, a future change would belong here.

## Fever shim

The Fever-compatible endpoint (`/fever`) uses a per-user random 32-byte token stored in the `fever_token` column. Constant-time compare in the auth path. Lazy-backfilled on first `/api/me` hit for users created before the column existed. The token is shown to the owning user only via `/api/me`; admin user lists never include it.

## CVE posture

- Go stdlib pinned to **1.26.3**.
- CI runs `go vet` and `govulncheck` on every push.
- Dependabot opens PRs weekly for `gomod` + `npm` updates.
