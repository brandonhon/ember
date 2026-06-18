# Ember

Self-hosted RSS/Atom reader. A single Go binary serving an embedded Svelte SPA, a JSON API + Fever shim, and a background poller that ingests feeds into SQLite (FTS5). Everything runs in containers.

_Primary repo on [GitHub](https://github.com/brandonhon/ember) (Releases + CI); mirrored to [Tangled](https://tangled.org/nodnarb.tngl.sh/ember)._

> **AI is fully optional.** Ember can summarize articles with a small local LLM via Ollama, but it's an opt-out feature, not a dependency. Set `EMBER_DISABLE_SUMMARIES=1` (or run the stack without the `ollama` sidecar) and the reader works exactly the same — no summary card, no model download, no inference, no LLM-related code paths. Even when enabled, everything runs on your own box; no article content leaves the host. Pick the deployment that matches your stance.

## Install

Three options, in order of effort. Each has a walkthrough in [docs/getting-started.md](docs/getting-started.md):

1. **Pre-built container** ([docs](https://brandonhon.github.io/ember/getting-started#run-from-the-released-container-image)) — `ghcr.io/brandonhon/ember:vX.Y.Z` (also `:X.Y`, `:X`, `:latest`). Multi-arch linux/amd64 + linux/arm64. Either `docker run` a single container to kick the tires, or swap the `build:` block in `deploy/docker-compose.yml` for `image: ghcr.io/brandonhon/ember:vX.Y.Z` to pull instead of building.
2. **Pre-built binary** ([docs](https://brandonhon.github.io/ember/getting-started#run-from-a-pre-built-binary)) — download from [Releases](https://github.com/brandonhon/ember/releases). Four tarballs (`linux-{amd64,arm64}`, `darwin-{amd64,arm64}`) + `SHA256SUMS`. Includes a sample `systemd` unit.
   ```sh
   VERSION=v0.8.4
   curl -L -o ember.tar.gz \
     "https://github.com/brandonhon/ember/releases/download/${VERSION}/ember-${VERSION}-linux-amd64.tar.gz"
   tar -xzf ember.tar.gz && ./ember --version
   ```
3. **From source** — see [Local development](#local-development).

## Quickstart (Docker)

```
cd deploy
cp .env.example .env
# Edit .env — set EMBER_SESSION_KEY (32+ random bytes) and EMBER_ADMIN_PASSWORD
docker compose up -d
```

Open `https://localhost` (Caddy serves the SPA + reverse-proxies the API). Log in with the admin credentials you set in `.env`.

On first boot:
1. The `ollama-pull` container fetches the configured model (default `qwen2.5:0.5b`).
2. The `ember` container creates the admin user from `EMBER_ADMIN_USER` / `EMBER_ADMIN_PASSWORD`.
3. The poller starts on a 60s tick (configurable via `EMBER_POLL_TICK`).

You'll land on an onboarding panel that points to starter packs or OPML import. Pick a pack and you're off.

## Features

### Reading
- Three-pane layout (sidebar / list / reader); single-pane drawer on mobile (≤900px).
- Smart views: Today, Fresh, All Unread, Starred, Read Later, Shared with me.
- Folders (categories) with rename, color, drag-to-reorder.
- Mute feeds; per-feed and aggregate unread badges; "!" badge on errored feeds.
- Cross-feed article dedup with "Also in N feeds" pill.
- Article actions: star, save for later, share (user / email / copy link), board pick.
- Reading-time estimate (200 wpm) on cards and in the reader.

### AI summaries
- Paragraph + bullet-point summary card in the reader.
- Per-user toggle for the summary card, plus install-time
  `EMBER_DISABLE_SUMMARIES` / `EMBER_DISABLE_IMAGES`.
- Admin-only LLM controls (Settings → Language model):
  - Auto-detected hardware recommendation (`ember probe`).
  - Switch active model live (no restart).
  - Pull / delete models from Ollama's cache.
  - Tuning sliders for temperature / top_p / num_ctx (persisted).
- AI ad-stripping: the model also returns a `CLEANED` body with newsletter
  signups, podcast/app promos, and social follow asks removed. Falls back to
  the original when the model can't produce a full body.

### Search + filters
- FTS5 full-text search; submitting from the topbar opens a dedicated results view.
- Saved searches: persist a query as a sidebar entry.
- Filter rules with `mark_read`, `star`, `hide`, `tag`, or `add_to_board` actions; eight match fields including feed, tags, `published_at`, and `has_image`; per-rule priority; Preview button counts last-7-day matches before save.
- "Mute" popover in the reader actions adds a hide-by-keyword rule in one click.
- Per-article user tags, with a `?tag=…` filter on the list endpoint.

### Onboarding + organization
- Five curated starter packs (Technology, Programming, Security, DevOps & Infra, World News).
- OPML import/export. Optional scheduled OPML export to `/data/exports/`.
- **Tiny Tiny RSS migration**: pull your subscriptions (recreating TT-RSS categories as folders) plus starred/archived articles from a running instance via its API, or upload an article export file. Already-subscribed feeds are skipped, so it's safe to re-run.
- **Subscribe by URL**: paste either a feed URL or just the homepage. Ember follows `<link rel=alternate>` and probes common feed paths (`/feed`, `/rss`, `/atom.xml`, `/feed.xml`, `/index.xml`).
- Drag-to-reorder feeds and folders.
- Mark-all-read at view / feed / category scope.

### Sign-in
- Password (argon2id) by default.
- **Passkeys / WebAuthn**: optional. Register from Settings → Passkeys; sign in with Touch ID / Face ID / hardware key. Requires `EMBER_PUBLIC_URL`.

### Daily digest email
- Opt-in nightly email summarizing your chosen view (Fresh / Today / Unread / Starred / Later).
- Pick the hour + minute in UTC, optionally override the From / To address.
- Configured via Settings → Daily digest. Requires the `EMBER_SMTP_*` env vars.

### Notifications + auto-refresh
- 15-second polling for new articles while the tab is visible (also fires on tab refocus).
- Canvas-rendered favicon with a green notification dot when unread items arrive.
- Page-title prefix `(N) Ember` so narrow tab strips show the count too.
- Installed as a PWA, new articles trigger an OS-level numeric badge on the app icon.

### Themes + branding
- 8 themes: Auto (matches OS), Light, Dark, Solarized, Sepia, Nord, Gruvbox, High contrast.
- Custom theme: pick 3 colors (paper/ink/ember); the rest is derived via CSS `color-mix()`.
- Admin branding: app name, browser-tab title, favicon URL.

### Admin
- `ember probe` subcommand reports RAM/CPU/GPU and recommends a model.
- Settings → Database: size, manual backup (VACUUM INTO), manual cleanup, schedules.
- Schedules persist in `app_settings` and run via an hourly maintenance goroutine.
- User management (create / update / delete / role).
- Resummarize-all to re-process every article after a prompt or model change.

### Other
- Reading stats: today/week/30-day, totals, top feeds.
- All confirmations use an in-app modal (no `window.confirm`).
- Fever-compatible mobile clients via `/fever`.
- WCAG 2.1 AA passes (axe-core via Playwright).
- PWA: manifest + service worker (cache-first assets, network-first `/api`).

## Configuration

### Required environment

| Var | Default | Purpose |
|---|---|---|
| `EMBER_SESSION_KEY` | _(required)_ | securecookie key (32+ bytes) |
| `EMBER_ADMIN_PASSWORD` | _(required first run)_ | first-run admin password |

### Optional environment

| Var | Default | Purpose |
|---|---|---|
| `EMBER_ADDR` | `:8080` | listen address |
| `EMBER_DB_PATH` | `/data/ember.db` | SQLite file |
| `EMBER_ADMIN_USER` | `admin` | first-run admin username |
| `EMBER_OLLAMA_URL` | `http://ollama:11434` | summarizer endpoint |
| `EMBER_OLLAMA_MODEL` | `qwen2.5:0.5b` | initial model (admin can swap later) |
| `EMBER_DISABLE_SUMMARIES` | `0` | skip LLM summarization entirely |
| `EMBER_DISABLE_IMAGES` | `0` | drop article hero images at ingest |
| `EMBER_FRESH_WINDOW` | `6h` | "Fresh" cutoff |
| `EMBER_POLL_CONCURRENCY` | `8` | poller workers |
| `EMBER_POLL_TICK` | `60s` | scheduler tick |
| `EMBER_POLL_MIN_INTERVAL` | `30m` | per-feed fetch floor ("check feeds every…"), 5m–24h; Settings → Feed check interval overrides at runtime |
| `EMBER_SESSION_TTL` | `24h` | session cookie lifetime (5m–90d); Settings → Sessions overrides at runtime |
| `EMBER_LOG_LEVEL` | `info` | slog level |
| `EMBER_TEST_MODE` | `0` | enables fake fetcher/summarizer for e2e |
| `EMBER_PUBLIC_URL` | _(unset)_ | canonical `scheme://host` users hit; required to enable passkey sign-in |
| `EMBER_ALLOW_PRIVATE_URLS` | `0` | bypass SSRF block to subscribe to RFC1918 / loopback feeds (only set if you trust every user who can add feeds) |
| `EMBER_SECURE_COOKIES` | `1` | `Secure` flag on session + CSRF cookies; set `0` only for deliberate plain-HTTP deployments |
| `EMBER_TRUSTED_PROXIES` | _(unset)_ | CIDRs/IPs of fronting proxy; `X-Real-IP` + `X-Forwarded-Proto` are honored only from these peers |
| `EMBER_HSTS_PRELOAD` | `0` | append `; preload` to the HSTS header; only set after submitting the domain to the preload list |
| `EMBER_SMTP_HOST` | _(unset)_ | SMTP host; required to enable daily-digest emails |
| `EMBER_SMTP_PORT` | `587` | SMTP port |
| `EMBER_SMTP_USER` | _(unset)_ | SMTP auth user (optional) |
| `EMBER_SMTP_PASSWORD` | _(unset)_ | SMTP auth password |
| `EMBER_SMTP_FROM` | _(unset)_ | digest `From:` address |
| `EMBER_SMTP_STARTTLS` | `1` | enable STARTTLS on submission ports |
| `EMBER_EMAIL_DOMAIN` | _(unset)_ | enable per-user newsletter inbox; host part of generated addresses (see [docs/email-inbox.md](docs/email-inbox.md)) |
| `EMBER_EMAIL_LISTEN_ADDR` | `:2525` | inbound SMTP bind address for the newsletter inbox |
| `EMBER_EMAIL_MAX_BYTES` | `26214400` | per-message size cap (25 MiB) |

### Runtime-tunable settings (Settings UI)

Stored in the `app_settings` KV; persist across restarts:

- Active LLM model + temperature / top_p / num_ctx
- Branding (name, page title, favicon URL)
- DB backup schedule (off | daily | weekly) + keep-N
- DB cleanup schedule (off | weekly | monthly) + window in days
- OPML export schedule (off | weekly | monthly)
- Session cookie TTL (overrides `EMBER_SESSION_TTL`)
- SMTP relay (host / port / user / password / from / STARTTLS) for the daily digest
- Initial feed-backlog window (default 48h)

## Local development

```
make web-install     # one-time
make test            # go tests
make web-test        # vitest
make embed           # build SPA + copy to internal/web/dist
make build           # produce ./bin/ember
EMBER_TEST_MODE=1 ./bin/ember   # listens on :8080 with the noop summarizer
```

Hot reload for the SPA:
```
cd web && npm run dev      # vite dev server, proxies /api → :8080
EMBER_TEST_MODE=1 ./bin/ember   # in another terminal
# visit http://localhost:5173
```

## Mobile clients

Reeder, FeedMe, and other Fever-compatible apps can connect via `/fever`. The `api_key` is `md5("<username>:<user_id>")` — see `/api/me` for your user_id. (We can't use the canonical `md5("user:pass")` because passwords are stored only as argon2id hashes.)

## E2E

```
make embed build              # produce ./bin/ember with the SPA embedded
cd web && npx playwright install chromium
npx playwright test           # spawns the binary in test mode against a temp DB
```

In test mode (`EMBER_TEST_MODE=1`) the binary seeds a deterministic admin (`admin` / `admintest`) plus 12 fixture articles and a single feed, so every spec has known data to assert against.

## Database

SQLite with WAL mode, 64 MiB cache, 256 MiB mmap, busy_timeout=5s, synchronous=NORMAL. Single connection — SQLite serializes writes, and the workload is small enough that the connection pool isn't a bottleneck. `PRAGMA optimize` runs after every startup migrate. Backups via `VACUUM INTO` are safe to run live.

Migration files live under `internal/db/migrations/` and are embedded into the binary.

## Architecture

See `docs/architecture.md` for the request lifecycle, poller state machine, and summarizer pipeline.
