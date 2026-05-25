# Ember

Self-hosted RSS/Atom reader. A single Go binary serving an embedded Svelte SPA, a JSON API + Fever shim, and a background poller that ingests feeds into SQLite (FTS5) and summarizes articles with a small local LLM via Ollama. Everything runs in containers.

See `EMBER_BUILD_PLAN.md` for the original implementation specification; this README is the current state of the project.

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
- Scroll-to-read auto-marks articles as you scroll past them.
- Article actions: star, save for later, share (user / email / copy link), board pick.
- Reading-time estimate (200 wpm) on cards and in the reader.

### AI summaries
- Paragraph + bullet-point summary card in the reader, with inline article thumbnail.
- Per-user toggles for summary card and images, plus install-time
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
- Filter rules with `mark_read`, `star`, or `hide` actions.
- "Mute" popover in the reader actions adds a hide-by-keyword rule in one click.
- Per-article user tags, with a `?tag=…` filter on the list endpoint.

### Onboarding + organization
- Five curated starter packs (Technology, Programming, Security, DevOps & Infra, World News).
- OPML import/export. Optional scheduled OPML export to `/data/exports/`.
- Drag-to-reorder feeds and folders.
- Mark-all-read at view / feed / category scope.

### Notifications + auto-refresh
- 30-second polling for new articles while the tab is visible.
- Green-dot favicon (`/icon-new.svg`) when unread items arrive.
- Page-title prefix `(N) Ember` so narrow tab strips show the count too.

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
- Server-Sent fresh-article notifications.
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
| `EMBER_LOG_LEVEL` | `info` | slog level |
| `EMBER_TEST_MODE` | `0` | enables fake fetcher/summarizer for e2e |

### Runtime-tunable settings (Settings UI)

Stored in the `app_settings` KV; persist across restarts:

- Active LLM model + temperature / top_p / num_ctx
- Branding (name, page title, favicon URL)
- DB backup schedule (off | daily | weekly) + keep-N
- DB cleanup schedule (off | weekly | monthly) + window in days
- OPML export schedule (off | weekly | monthly)

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

See `docs/ARCHITECTURE.md` for the request lifecycle, poller state machine, and summarizer pipeline.
