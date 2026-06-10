# Configuration

Ember reads configuration from environment variables at startup. A handful of settings can also be changed at runtime via the admin UI and persist in the `app_settings` table.

## Required env vars

| Var | Description |
| --- | --- |
| `EMBER_SESSION_KEY` | securecookie key, 32+ random bytes. Generate via `openssl rand -base64 48`. |
| `EMBER_ADMIN_PASSWORD` | First-run admin password. Used only when the `users` table is empty; change via Settings → Profile after first login. **Must be at least 8 characters** — a shorter or empty value makes the container **exit on startup** with `bootstrap admin: …must be at least 8 characters` rather than starting with no usable account. |

## Optional env vars

| Var | Default | Notes |
| --- | --- | --- |
| `EMBER_ADDR` | `:8080` | Bind address. |
| `EMBER_DB_PATH` | `/data/ember.db` | SQLite file path. |
| `EMBER_ADMIN_USER` | `admin` | First-run admin username. |
| `EMBER_OLLAMA_URL` | `http://ollama:11434` | Ollama API endpoint. |
| `EMBER_OLLAMA_MODEL` | `qwen2.5:0.5b` | Initial active model. The admin UI can swap to any pulled model live. |
| `EMBER_DISABLE_SUMMARIES` | `0` | Skip LLM summarization entirely. Articles still surface (poller stamps `summary_model='disabled'`). |
| `EMBER_DISABLE_IMAGES` | `0` | Drop article hero images at ingest. |
| `EMBER_ALLOW_PRIVATE_URLS` | `0` | Bypass the SSRF block so feeds on RFC1918 / loopback addresses can be subscribed. **Only set this if you trust every user who can add feeds.** |
| `EMBER_PUBLIC_URL` | — | Canonical `scheme://host` users hit, e.g. `https://reader.example.com`. Required to enable passkey / WebAuthn sign-in. |
| `EMBER_SECURE_COOKIES` | `1` | `Secure` flag on the session + CSRF cookies. Ember serves plain HTTP and expects a TLS-terminating proxy in front, so leave this on. Set `0` **only** for a deliberate plain-HTTP deployment (e.g. private VPN) — otherwise browsers drop the cookies over HTTP and login silently fails. |
| `EMBER_TRUSTED_PROXIES` | — | Comma/space-separated CIDRs or IPs of the proxy in front of Ember. `X-Real-IP` (rate-limit keying) and `X-Forwarded-Proto` (HTTPS detection for HSTS) are honored **only** from these peers. Empty = trust nobody (Ember is the edge; reads the real client from the connection). The bundled compose sets this to the Caddy bridge range. |
| `EMBER_SMTP_HOST` | — | SMTP server hostname. Required to enable the daily-digest email feature. Can also be set per-server in **Settings → Email / SMTP** (admin), which takes precedence. |
| `EMBER_SMTP_PORT` | `587` | SMTP port. Overrideable in **Settings → Email / SMTP**. |
| `EMBER_SMTP_USER` | — | SMTP auth username (optional; omit for relays without auth). Overrideable in **Settings → Email / SMTP**. |
| `EMBER_SMTP_PASSWORD` | — | SMTP auth password. Overrideable in **Settings → Email / SMTP** (stored write-only — never echoed back to the UI). |
| `EMBER_SMTP_FROM` | — | `From:` address used on digest emails. Overrideable in **Settings → Email / SMTP**. |
| `EMBER_SMTP_STARTTLS` | `1` | STARTTLS on submission ports (587). When on, the server **must** offer STARTTLS or the send fails (no silent plaintext downgrade). Set `0` only for a **loopback** relay (`localhost` / `127.0.0.1` / `::1`) — plain SMTP to any remote host is refused so credentials never cross the network in the clear. Overrideable in **Settings → Email / SMTP**. |
| `EMBER_FRESH_WINDOW` | `6h` | How recent an article must be to appear in the "Fresh" smart view. Fresh lists recent **unread** articles, matching its sidebar badge. |
| `EMBER_POLL_CONCURRENCY` | `8` | Number of feed-fetch worker goroutines. |
| `EMBER_POLL_TICK` | `60s` | How often the poller scans for feeds due to fetch. |
| `EMBER_POLL_MIN_INTERVAL` | `30m` | Floor for the adaptive per-feed fetch interval ("check feeds every…"). Active feeds settle near this value; quiet feeds back off up to 6h. Range-validated **5m–24h**. Admins can change it at runtime in **Settings → (Email/Data) → Feed check interval** (persisted in `app_settings`, overrides this env default, no restart needed). |
| `EMBER_SESSION_TTL` | `24h` | Lifetime of a freshly-issued session cookie. Go duration (e.g. `30m`, `12h`, `168h`). Range-validated (5m–90d). Admin UI override in **Settings → Sessions** takes precedence when set. |
| `EMBER_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error`. |
| `EMBER_TEST_MODE` | `0` | Seeds fake admin + articles; loosens cookie Secure flag; and (when `EMBER_SESSION_KEY` is unset) falls back to a **hardcoded, publicly-known session signing key** — session cookies become forgeable. Logs a loud warning at startup. **Never enable in production.** |
| `EMBER_HSTS_PRELOAD` | `0` | Appends `; preload` to the `Strict-Transport-Security` header. Long-term, browser-level commitment — only set after the domain is (or will be) submitted to the [HSTS preload list](https://hstspreload.org). |
| `EMBER_EMAIL_DOMAIN` | — | Enables the per-user newsletter inbox. Without it, the SMTP listener doesn't start and the inbox endpoints return `enabled: false`. See [Email newsletter inbox](/email-inbox) for the operator setup. |
| `EMBER_EMAIL_LISTEN_ADDR` | `:2525` | Inbound SMTP bind address for the newsletter inbox. Front with Caddy layer4 / haproxy / nginx stream to publish on port 25. |
| `EMBER_EMAIL_MAX_BYTES` | `26214400` (25 MiB) | Per-message size cap on inbound newsletter mail. |

## Runtime settings (persist across restarts)

Stored in the `app_settings` KV. Edit via the admin UI in **Settings → ...**.

| Setting | Where to change |
| --- | --- |
| Active LLM model | Language model |
| Temperature / Top P / Context window | Language model → Tuning |
| Session cookie TTL (overrides `EMBER_SESSION_TTL`) | Sessions |
| App name, page title, favicon URL | Branding |
| Backup schedule + retention (`db_backup_keep`, default 7) | Database |
| Cleanup schedule + window (`db_cleanup_older_days`, default 90) | Database |
| OPML export schedule + retention (`opml_keep`, default 12) | Database |
| SMTP host / port / username / password / from / STARTTLS | Email / SMTP |
| Initial feed-backlog window (default **24 hours**; 0 = no gate) | Email / SMTP → Initial backlog window |
| Reading window (default 24h; range 24–168h) | Feeds → Reading window |
| Search window (default 48h; range 24–168h) | Feeds → Search window |

### Time windows, retention, and counts

Ember keeps articles for a **fixed rolling 1-week (168h) retention window**, then prunes
them automatically (a daily delete-only sweep). Starred, Read-Later, board-pinned, and
shared articles are exempt and kept indefinitely. This retention window is **not**
configurable — it's the hard ceiling for the two settings below, because you can't surface
what's already been pruned.

- **Ingest** — a brand-new feed pulls only the last 24h on its first fetch (the *initial
  backlog window*). Existing feeds only add genuinely new items (GUID + content-hash dedup),
  so reposts don't reappear.
- **Reading window** (default 24h, extendable to 168h) — the *floor* for how far back the
  unread views and counts reach. *Today* always shows exactly this window, newest first.
  A feed, a category, and *All Unread* show at least this window but **extend back to your
  previous login** when you've been away longer (so time away surfaces everything new since),
  clamped at the retention window. Older articles stay in the database (searchable) but are
  hidden from these views. Because a feed/category list and its sidebar badge share this one
  cutoff, the badge counts exactly the set the column pages through — the list loads 50 at a
  time with a **Load more** button (search loads 25 at a time), so the badge is the running
  total and "Load more" reveals the rest.
- **Search window** (default 48h, extendable to 168h) — full-text search matches articles
  published within this window. Results older than 24h are tagged with a date pill showing
  when they were polled. The window can't exceed retention — that's the safeguard.

Sidebar badges (All Unread, per-folder, per-feed) are computed server-side with the **same**
window, summary gate, and cross-feed dedup as the article list, so a badge always matches the
column it summarizes. When AI summarization is enabled, articles are hidden from every view
and count until the summarizer has processed them; when it's disabled, nothing is gated.

Each user also has client-side preferences stored in browser `localStorage`:

| Preference | Default | Key |
| --- | --- | --- |
| Theme | Auto (OS) | `ember:theme` |
| Article density | Cards | `ember:density` |
| Sidebar collapsed | Open | `ember:sidebar` |
| AI summary card visible | On | `ember:show-summary` |
| Summary card collapsed | Open | `ember:summary-collapsed` |
| Custom theme palette | n/a | `ember:custom` |

## Hardware-aware model recommendation

Run `ember probe` (or open the admin Language model page) to see a recommendation based on detected RAM, CPU count, and GPU.

| RAM | GPU | Recommended |
| --- | --- | --- |
| < 2 GiB | — | Disable summaries |
| 2–4 GiB | — | `qwen2.5:0.5b` |
| 4–8 GiB | — | `qwen2.5:0.5b` |
| 8–16 GiB | — | `qwen2.5:1.5b` |
| 16 GiB+ | — | `qwen2.5:3b` |
| any | NVIDIA / Apple Silicon | `qwen2.5:7b` |

## Stack-level env vars (docker-compose)

These configure the bundled `deploy/docker-compose.yml` stack rather than the Ember binary itself.

| Var | Default | Notes |
| --- | --- | --- |
| `EMBER_HOSTNAME` | `localhost` | Hostname Caddy serves. Real DNS name → automatic Let's Encrypt. |
| `CADDY_EMAIL` | `admin@localhost` | Email registered with Let's Encrypt for ACME notifications. Set this when using a real hostname. |
| `EMBER_HTTP_PORT` | `80` | Host port Caddy publishes for HTTP. Change when 80 is taken locally. |
| `EMBER_HTTPS_PORT` | `443` | Host port Caddy publishes for HTTPS. Change when 443 is taken locally. |
| `EMBER_DISABLE_HTTPS_REDIRECT` | _(unset)_ | Set to `1` to turn off Caddy's default 80 → 443 redirect. Use when an upstream proxy already terminates TLS. |

If you change the ports, reach the site at the mapped port — e.g. `EMBER_HTTPS_PORT=8443` → visit `https://localhost:8443`. Inside the container Caddy still listens on 80/443; only the host-side mapping changes.

### Let's Encrypt

Caddy fetches a free Let's Encrypt cert automatically when **all three** of these are true:

1. `EMBER_HOSTNAME` is a real DNS name that resolves to this server (e.g. `ember.example.com`).
2. `CADDY_EMAIL` is a valid email address.
3. The public internet can reach port `80` (HTTP-01 challenge) or port `443` (TLS-ALPN-01 challenge) on this host.

If you remap `EMBER_HTTP_PORT` / `EMBER_HTTPS_PORT` *and* expect Let's Encrypt to issue certs, ensure your public ingress (router / upstream proxy / Cloudflare) still terminates on 80/443 and forwards to your mapped host ports. For homelab use with `tls internal` in the Caddyfile, any ports work fine.

### HTTP → HTTPS redirect

Caddy redirects port 80 to 443 by default for any site with managed TLS. Set `EMBER_DISABLE_HTTPS_REDIRECT=1` in `.env` to turn this off — needed when Ember sits behind another reverse proxy (Traefik, nginx, Cloudflare Tunnel, etc.) that already handles the redirect or terminates TLS upstream.

## Reverse proxy

Ember expects TLS to be terminated upstream. The reference `Caddyfile` in `deploy/Caddyfile` covers:

- Automatic Let's Encrypt for a real hostname (default).
- `tls internal` for self-signed homelab certs.
- `EMBER_DISABLE_HTTPS_REDIRECT` toggle for the 80 → 443 redirect.
- Forwarding `X-Real-IP` + `X-Forwarded-Proto` (Ember honors these **only** from peers listed in `EMBER_TRUSTED_PROXIES`; the bundled compose sets it to the Caddy bridge range). Without it, Ember treats itself as the edge and ignores both headers.

If you front Ember with Cloudflare, set the [authenticated origin pull](https://developers.cloudflare.com/ssl/origin-configuration/authenticated-origin-pull/) so only Cloudflare can reach your origin.
