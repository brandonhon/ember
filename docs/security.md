# Security

Ember has been written with the assumption that it will be exposed to the public internet behind a reverse proxy. This page lists the defenses in place and the deployment expectations.

For vulnerability reporting, see [SECURITY.md](https://github.com/brandonhon/ember/blob/main/SECURITY.md). Don't open public issues for security problems.

## Authentication

- **Passwords**: argon2id (`time=3`, `memory=64 MiB`, `parallelism=2`, salt=16 bytes). Meets OWASP 2024 recommendations. Concurrent derivations are capped process-wide (4 at a time) so a login flood queues instead of multiplying 64 MiB allocations into an out-of-memory kill; excess requests wait on a slot rather than each reserving memory.
- **Login throttling**: in addition to the per-IP bucket below, each *username* gets an escalating backoff — the first 5 consecutive failures are free, then each further attempt doubles a mandatory wait (1s, 2s, 4s…) capped at 60 seconds, returned as `429` with a `Retry-After` header. This is what bounds a distributed credential-stuffing run, which a per-IP limit alone does not: the attacker can rotate source addresses but not the account being attacked. The counter keys on the submitted username whether or not the account exists, so the throttle can't be used to enumerate accounts, and a successful login clears it immediately. Deliberately a delay and not a lockout — a hard lock would let anyone who knows a username deny that account service indefinitely. Failures past the free allowance are logged with the username and client IP for alerting.
- **Sessions**: 64-hex-char random IDs signed via `gorilla/securecookie`. Server-side row in `sessions` table backs every cookie. Default **idle timeout** 24 hours (max gap between requests); active sessions slide forward on each request, capped at **30 days from login** before re-authentication is required. Override the idle timeout via `EMBER_SESSION_TTL` env var or **Settings → Sessions** (bounded 5 min – 90 days). Session cookies are persistent, so they survive a browser restart but a private/incognito window discards them when its last window closes. Destroyed on logout, login (fixation defense), and on password change (both self-service and admin).
- **Cookies**: `HttpOnly`, `Secure`, `SameSite=Strict`, scoped to `/`.
- **Rate limiting**: per-IP token bucket on `POST /api/auth/login` and `POST /api/auth/passkey/*` (burst 10/min). A second, higher-burst bucket (30/min) guards the expensive authenticated endpoints — add-feed, feed discovery, edit-feed (URL repoint), refresh-feed, refresh-all-feeds, resummarize(-all), OPML import, TT-RSS import (file + live API), starter-pack import, article extract, the image proxy (`GET /api/img`), and search — which spawn outbound fetches / goroutines / FTS work. "Refresh all" additionally bounds its detached refresh goroutines with a global semaphore so concurrent requests can't pile work onto the single SQLite writer. Buckets key on the real client IP (see Transport / proxy expectations for how that's resolved). The bucket table is capped (10,000 keys) with idle entries swept on demand, so a host spraying an entire IPv6 range can't grow it without bound; the sweep itself is rate-limited to once a second so the cap can't be turned into a source of quadratic work.
- **Passkeys / WebAuthn**: optional second sign-in method (FIDO2). Credentials are bound to a relying-party ID derived from `EMBER_PUBLIC_URL`; ceremonies expire after 5 minutes; a stale `webauthn_sessions` row is reaped on a 15-minute cadence. Credentials never leave the device — only the public key is stored. The passkey login-begin path runs a throwaway credential lookup on an unknown username so its response timing matches the found-user path (no username enumeration). An assertion whose signature counter fails to advance past the stored value is **rejected**, not merely noted — that is the spec's cloned-authenticator signal — and the stored high-water mark is left untouched so a replay can't launder itself into looking like an advance. Authenticators that never implement a counter (they report `0` every time) are exempt, as the spec requires. User verification is `preferred` by default and can be raised to `required` per instance via `EMBER_PASSKEY_REQUIRE_UV` or **Settings → Passkeys**; the requirement is recorded in the server-side ceremony row, so a browser can't negotiate it away between begin and finish.
- **Account email**: the self-service email change (`PATCH /api/me/email`) requires the current password (re-auth), so a stolen session can't silently redirect the account's digest mail. Email addresses are unique (case-insensitive) so two accounts can't converge on one address.

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

Scoping reads isn't sufficient where a request body carries a **reference to another row** — a foreign key the caller chooses. Those are checked for ownership on the way in, not just filtered on the way out: `category_id` on `POST /api/feeds` and `PATCH /api/feeds/{id}`, and `board_id` / `category_id` on `POST /api/articles/mark-all-read`, all resolve the id against the caller's own rows first and 404 on anything else. A database foreign key proves only that the row exists, never whose it is.

The admin file-management endpoints that take a filename — `DELETE /api/admin/db/backups/{name}` and `…/exports/{name}` — reject any `{name}` that isn't a bare basename with the expected extension (`.db` / `.opml`), so a crafted value can't traverse out of the configured backup/export directory. Per-user filter import (`POST /api/filters/import`) validates each rule through the same `ParseMatch` / `ValidateActionWithValue` path as a manual create and silently skips anything invalid or beyond the per-user cap, so a hand-edited bundle can't inject malformed rules.

## CSRF

Double-submit pattern. A random `ember_csrf` cookie (16 `crypto/rand` bytes hex-encoded, `SameSite=Strict`, not HttpOnly so the SPA can echo it) must be echoed in the `X-Ember-CSRF` header on every state-changing request (`POST`, `PATCH`, `DELETE`). The header/cookie compare is constant-time.

Two safe exceptions:

- `POST /api/auth/login` — bootstrap, no session cookie yet.
- Anonymous requests to `GET` endpoints — no session means nothing to forge.

The login bypass uses **exact path match**, not suffix match.

## SSRF protection

Every outbound URL fetch passes through `internal/urlcheck.Check`:

- Scheme allowlist: `http`, `https`. Everything else (`file`, `gopher`, `javascript`) is refused.
- Private-IP block: literal IPs and DNS resolutions inside RFC1918 (`10/8`, `172.16/12`, `192.168/16`), loopback (`127/8`, `::1`), link-local (`169.254/16` — also the AWS / GCP / Azure metadata endpoint — and `fe80::/10`), CGNAT (`100.64/10`), unspecified (`0.0.0.0/8`), IPv6 ULA (`fc00::/7`), 6to4 (`2002::/16`), NAT64 (`64:ff9b::/96`), and the whole of `::/96` are refused.
  - `::/96` covers the unspecified address (`::`) and the deprecated but still-parsed IPv4-**compatible** form (`::a.b.c.d`). Both were real bypasses: Go's `net.IP.To4()` normalizes only the IPv4-**mapped** form (`::ffff:a.b.c.d`), so `http://[::127.0.0.1]/` never matched `127.0.0.0/8` while the dial still reached loopback, and `http://[::]/` matched no block at all. Nothing routable lives in `::/96`, so the range is blocked wholesale. Guarded by a regression test.
- Port block: well-known non-web service ports (SSH `22`, SMTP `25`, Redis `6379`, Memcached `11211`, common databases, …) — a curated subset of the WHATWG "bad ports" list — are refused. A feed always lives on a web port, so this closes a blind port-scan oracle without affecting real feeds, and it applies even under `EMBER_ALLOW_PRIVATE_URLS`.
- Redirect chains: feed fetcher rejects 30x to private addresses via `feed.RedirectGuard`. The Ollama summarizer client refuses to follow redirects at all (its base URL is admin-set and often loopback, so it gets the redirect guard but not the private-IP block).
- Opt-in bypass: `EMBER_ALLOW_PRIVATE_URLS=1` skips the IP check for homelabs that need LAN feeds. Scheme allowlist still applies.

Surfaces covered:

- `POST /api/feeds` (add feed)
- `PATCH /api/feeds/{id}` (edit feed — a changed source URL is validated and re-fetched before the subscription is re-pointed; the route is rate-limited like add-feed)
- `POST /api/feeds/discover` (multi-feed picker — the target page and every advertised feed link is validated before fetching)
- `POST /api/feeds/import` (OPML import — each `xmlUrl` is filtered)
- `POST /api/feeds/import-ttrss-api` (TT-RSS live pull / full migrate — the API endpoint is validated and the client carries `feed.RedirectGuard`; the user-supplied TT-RSS credentials are held only for the request and never persisted). When subscriptions are migrated, every feed URL returned by `getFeeds` is run through `urlcheck.Check` before subscribing (a blocked URL is skipped, not fatal). The file upload `POST /api/feeds/import-ttrss` makes no outbound request, and `javascript:`/`data:` article links in the export are dropped.
- Poller readability enrichment (Lobsters / HN aggregator → external link)
- Feed fetcher redirects
- `POST /api/articles/{id}/extract` (on-demand Re-extract) — runs the same `urlcheck.Check` before fetching the article URL through readability.
- `GET /api/img` (same-origin image proxy) — the requested URL must carry a valid HMAC signature the server issued when it rewrote `image_url` (so a client can't point the proxy at an arbitrary host), and is then run through `urlcheck.Check` before fetching. Only `image/*` responses are streamed back, size- (10 MiB) and time- (15 s) bounded. The outbound client uses `urlcheck.GuardedTransport`, so the private-IP block also covers the actual dial.

The admin-only `POST /api/admin/settings/email-test` opens an SMTP TCP connection to the admin-supplied `host:port`. This is **by design**: the same connection happens every 5 minutes from the digest sender when SMTP is configured. Access is gated by `is_admin = 1` so a non-admin can't use it as a port-scan or relay-probe primitive. On failure the endpoint returns a **generic** message; the underlying SMTP/TLS/DNS error (which can carry server banners, internal hostnames, or AUTH fragments) is logged server-side only.

## SMTP transport

Outbound mail (digests + the test message) never sends credentials or message bodies in cleartext across the network:

- `EMBER_SMTP_STARTTLS=1` (default) **requires** STARTTLS — if the server doesn't advertise it (or a MitM strips it from the EHLO response), the send fails rather than downgrading to plaintext. The TLS handshake pins `MinVersion: TLS 1.2` and verifies the server certificate.
- `EMBER_SMTP_STARTTLS=0` (plain SMTP) is permitted **only** to a loopback host (`localhost` / `127.0.0.1` / `::1`) — a local relay or sidecar. Plain SMTP to any remote host is refused before the connection opens.
- **MIME boundaries are unpredictable.** Digest messages are `multipart/alternative`, and the text part writes feed and article titles verbatim (only the HTML part escapes them). A guessable boundary would therefore let a crafted title close the part early and forge extra MIME sections, so the boundary comes from `crypto/rand` and a failure to generate one aborts the send rather than falling back to a fixed string.

## Update check

Ember can tell admins when a newer release exists. The mechanism is deliberately minimal and privacy-preserving:

- **No telemetry.** The only outbound call is an unauthenticated `GET https://api.github.com/repos/brandonhon/ember/releases/latest`. Nothing about your instance — no version ping, no user data, no identifier — is sent. The request carries a static `User-Agent` and reads only the latest tag, release URL, and publish date.
- **Fixed host.** The target is a hardcoded GitHub API URL, so the SSRF surface is nil; there is no user-controlled address to validate.
- **Admin-only.** The cached result is surfaced through `/api/me` **only** to `is_admin` users (updating the container is an operator action). Non-admin readers never receive it.
- **Release builds only, once a day.** The check runs only on a clean tagged build (dev/dirty builds skip it entirely) and polls at most once every 24 h, far under GitHub's 60 req/hr/IP unauthenticated limit. A failed check logs and keeps the previous result — it never blocks the app.
- **Opt-out.** Set `EMBER_DISABLE_UPDATE_CHECK=1`, or toggle **Settings → Check for updates** off at runtime, to stop the outbound call.

## Body limits

- `decodeJSON` wraps the body in `http.MaxBytesReader` capped at **1 MiB**.
- OPML import body capped at **8 MiB** (and the parser reads at most 10 MiB).
- TT-RSS file import capped at **50 MiB** (the streaming XML parser reads at most that); the live API pull paginates and stops at 100k articles, and the subscription migration paginates `getFeeds` and stops at 10k feeds.
- Fever (`/fever`) form body capped at **64 KiB**.
- Request headers capped at **64 KiB** (`http.Server.MaxHeaderBytes`).
- `/api/articles/read` (and other bulk endpoints) accept at most **1000** ids per request; `/api/articles?limit=` is clamped to **200**.
- Readability extraction (poller enrichment + `POST /api/articles/{id}/extract`) caps the fetched page at **8 MiB** before parsing; the feed fetcher caps response bodies at **16 MiB**.
- Each decoded inbound-email MIME part is capped at **16 MiB** (base64 expands ~4:3, so a near-limit message can't balloon past the 25 MiB message ceiling).
- `GET /api/search` clamps `limit` to **100** and `offset` to **10000**.

## Feed error messages

A feed's last fetch error is shown in the sidebar tooltip and returned by `GET /api/feeds`, which every **subscriber** of that feed can read — not just admins. Raw Go transport errors embed whatever the connection resolved to (`dial tcp 10.0.0.5:443: connect: connection refused`), and a redirect chain can add internal hostnames, so the stored value is a classified summary rather than the raw error:

- the publisher's HTTP status is kept verbatim (`the server responded 404`) — it's their response, and it's the most useful thing to show;
- transport failures collapse to a category — `could not connect to the server`, `the server took too long to respond`, `could not find that domain`, `the server's TLS certificate could not be verified`, `too many redirects`;
- parse failures report `the response was not a valid RSS or Atom feed`;
- Ember's own SSRF guard reports why it refused (private/loopback address, non-http scheme, disallowed port).

The mapping is **fail-closed**: an error shape it doesn't recognise reports a generic `the feed could not be fetched` rather than falling through to the raw text, so a new error from a dependency can't silently start leaking. Operators lose nothing — the poller logs the unmodified error at both call sites.

## Error responses

5xx responses always read `{"error": {"code": "internal", "message": "internal error"}}`. The actual error is logged server-side via `slog.Default().Error(...)`. No SQLite errors, file paths, or constraint details leak to clients. OPML / TT-RSS import and multipart-parse failures likewise return a generic 400 ("could not read the uploaded file" / "check the file is a valid … export"); the underlying XML offset, SQLite constraint, or temp-path detail is logged only.

Ollama upstream errors (502 from `/api/admin/llm/pull` etc.) return generic "Ollama refused the pull" messages; the upstream text goes to logs.

## Transport / proxy expectations

Ember serves **plain HTTP only** — it has no TLS listener. The supported deployment terminates TLS in a fronting proxy (Caddy, in the bundled compose) and forwards to `:8080`. Ember's `:8080` should never be reachable directly from the internet.

- **Trusted proxies**: `X-Real-IP` (rate-limit keying) and `X-Forwarded-Proto` (HTTPS detection) are honored **only** from peers in `EMBER_TRUSTED_PROXIES`. The default is **empty — trust nobody**: Ember treats itself as the edge and reads the real client from the connection, ignoring those headers. Set `EMBER_TRUSTED_PROXIES` to your proxy's address/range (the bundled compose sets it to the Caddy bridge range). This closes the spoofing gap where a same-LAN or same-Docker-network peer could forge `X-Real-IP` to bypass the limiter or poison logs.
- **HSTS** is emitted **only over HTTPS** — when the request arrives over TLS, or a trusted proxy reports `X-Forwarded-Proto: https`. Browsers ignore HSTS received over plain HTTP (RFC 6797), so it is never sent on a bare-HTTP connection.
- **Cookies** carry `Secure` by default. Behind a TLS proxy that's correct. For a deliberate plain-HTTP deployment (e.g. private VPN) set `EMBER_SECURE_COOKIES=false`, otherwise browsers drop the cookie over HTTP and login silently fails. Ember logs a startup warning when it's bound to a non-loopback address with `Secure` cookies on and no trusted proxy configured.
- **Static files never render a directory listing.** Go's `http.FileServer` generates an index page for any directory without an `index.html`, which exposed every built asset filename at `/assets/` — and since that prefix carries a year-long `immutable` cache header, the listing was cached as though it were a content-hashed file. Directory requests now return 404, checked before any cache header is applied. SPA client-side routes are unaffected (they fall through to `index.html` as before).
- `Permissions-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, COOP, CORP, and a locked-down CSP (`default-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'`) are set on every response, including 404/405 and error responses. The CSP intentionally allows `img-src https:` (inline `<img>` inside article bodies still load from third-party publisher CDNs; the card/reader lead image is proxied same-origin via `/api/img` — see Image proxy below — but body images are not), `img-src data:` (the dynamic tab favicon, a canvas-rasterized `data:image/png` with the unread-dot overlay), and `style-src 'unsafe-inline'` (the SPA's scoped styles) — accepted trade-offs for a self-hosted reader.

## Image proxy

The card thumbnail and the reader's lead image are served from Ember's own origin via `GET /api/img` rather than fetched directly from the publisher's CDN. Content/tracker blockers (uBlock Origin, Privacy Badger, …) match on those CDN domains and would otherwise silently strip the lead image (e.g. Fox News' `a57.foxnews.com`); routed same-origin it loads normally.

- **Capability, not an open relay**: the API rewrites `image_url` to `/api/img?u=<src>&s=<sig>` at serve time, where `sig` is an HMAC over `src` keyed by a value derived from `EMBER_SESSION_KEY` (domain-separated so it can't be confused with session/CSRF use). `/api/img` verifies the signature in constant time before any network I/O, so a client can't aim the proxy at an arbitrary host.
- **Bounded + guarded**: the source URL also passes `urlcheck.Check`, and the outbound client uses `urlcheck.GuardedTransport` (private-IP block on the real dial). Only `image/*` responses are returned, capped at 10 MiB and a 15 s timeout.
- **Stateless**: stream-through with a 1-day `Cache-Control` (browser/proxy cache); no server-side image cache. Scoped to the lead image — inline body `<img>` are left pointing at their origin CDN, which is why `img-src https:` stays in the CSP.

## Database

- SQLite WAL with single-writer semantics. The write handle is capped at `MaxOpenConns=1` to avoid `SQLITE_BUSY` storms; a second, read-only handle (`query_only`, 4 connections) serves heavy read queries alongside it, and SQLite itself rejects any write attempted on that handle.
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

`unread_item_ids` / `saved_item_ids` return the user's complete id set (a Fever client diffs it against local state) — they are intentionally uncapped, so the response scales with the per-user unread/saved backlog (small integers; authenticated and per-user). Bulk item content is fetched only through the paginated `items` surface (`since_id` / `max_id` / `with_ids`, ≤ 50 per call).

## CVE posture

- Go stdlib pinned to **1.26.4**.
- CI runs `go vet`, `golangci-lint`, and `govulncheck` on every push.
- Dependabot opens PRs weekly for `gomod` + `npm` updates.
