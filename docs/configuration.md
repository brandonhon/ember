# Configuration

Ember reads configuration from environment variables at startup. A handful of settings can also be changed at runtime via the admin UI and persist in the `app_settings` table.

## Required env vars

| Var | Description |
| --- | --- |
| `EMBER_SESSION_KEY` | securecookie key, 32+ random bytes. Generate via `openssl rand -base64 48`. |
| `EMBER_ADMIN_PASSWORD` | First-run admin password. Used only when the `users` table is empty; change via Settings → Profile after first login. |

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
| `EMBER_SMTP_HOST` | — | SMTP server hostname. Required to enable the daily-digest email feature. |
| `EMBER_SMTP_PORT` | `587` | SMTP port. |
| `EMBER_SMTP_USER` | — | SMTP auth username (optional; omit for relays without auth). |
| `EMBER_SMTP_PASSWORD` | — | SMTP auth password. |
| `EMBER_SMTP_FROM` | — | `From:` address used on digest emails. |
| `EMBER_SMTP_STARTTLS` | `1` | STARTTLS on submission ports (587). Set `0` only when targeting a server that doesn't support it. |
| `EMBER_FRESH_WINDOW` | `6h` | How recent an article must be to appear in the "Fresh" smart view. |
| `EMBER_POLL_CONCURRENCY` | `8` | Number of feed-fetch worker goroutines. |
| `EMBER_POLL_TICK` | `60s` | How often the poller scans for feeds due to fetch. |
| `EMBER_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error`. |
| `EMBER_TEST_MODE` | `0` | Seeds fake admin + articles; loosens cookie Secure flag. Don't enable in production. |

## Runtime settings (persist across restarts)

Stored in the `app_settings` KV. Edit via the admin UI in **Settings → ...**.

| Setting | Where to change |
| --- | --- |
| Active LLM model | Language model |
| Temperature / Top P / Context window | Language model → Tuning |
| App name, page title, favicon URL | Branding |
| Backup schedule + retention (`db_backup_keep`, default 7) | Database |
| Cleanup schedule + window (`db_cleanup_older_days`, default 90) | Database |
| OPML export schedule + retention (`opml_keep`, default 12) | Database |

Each user also has client-side preferences stored in browser `localStorage`:

| Preference | Default | Key |
| --- | --- | --- |
| Theme | Auto (OS) | `ember:theme` |
| Article density | Cards | `ember:density` |
| Sidebar collapsed | Open | `ember:sidebar` |
| AI summary card visible | On | `ember:show-summary` |
| Article images visible | On | `ember:show-images` |
| Summary card collapsed | Open | `ember:summary-collapsed` |
| Scroll-to-mark-read | On | `ember:scroll-mark-read` |
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
| `CADDY_EMAIL` | `admin@localhost` | Email for ACME registration. |
| `EMBER_HTTP_PORT` | `80` | Host port Caddy publishes for HTTP. Change when 80 is taken locally. |
| `EMBER_HTTPS_PORT` | `443` | Host port Caddy publishes for HTTPS. Change when 443 is taken locally. |

If you change the ports, reach the site at the mapped port — e.g. `EMBER_HTTPS_PORT=8443` → visit `https://localhost:8443`. Inside the container Caddy still listens on 80/443; only the host-side mapping changes.

**Let's Encrypt caveat:** automatic-HTTPS via Let's Encrypt's HTTP-01 challenge requires the **public** port 80 to be reachable; TLS-ALPN-01 requires public 443. If you remap *and* expect Let's Encrypt to issue certs, ensure your public ingress (router / upstream proxy / Cloudflare) still terminates on 80/443 and forwards to your mapped host ports. For homelab use with `tls internal` in the Caddyfile, any ports work fine.

## Reverse proxy

Ember expects TLS to be terminated upstream. The reference `Caddyfile` in `deploy/Caddyfile` covers:

- Automatic Let's Encrypt for a real hostname.
- `tls internal` for self-signed homelab certs.
- Forwarding `X-Real-IP` (Ember honors this header **only** when the immediate peer is loopback / Docker / LAN).

If you front Ember with Cloudflare, set the [authenticated origin pull](https://developers.cloudflare.com/ssl/origin-configuration/authenticated-origin-pull/) so only Cloudflare can reach your origin.
