# Architecture

A reference for contributors. Covers process layout, request lifecycle, and how the major subsystems hand data off.

## Process layout

```
caddy ─┬─> ember (Go binary)
       │     ├─ HTTP API + Fever shim + SPA serve
       │     ├─ Background poller (per-feed adaptive ticker)
       │     ├─ Summary worker pool (Ollama HTTP client)
       │     └─ DB maintenance goroutine (backup / cleanup / OPML / hourly)
       │
       └─> SPA (embedded static files served by ember)

ollama  (sidecar) — model storage + inference
```

The single `ember` binary embeds the Svelte SPA via `embed.FS` and serves it under `/`. The API is `/api/*`; mobile clients use `/fever`. The poller, summary worker, and maintenance goroutine all share the database via `internal/store`.

## Package map

```
cmd/ember/                    main + probe subcommand + DB maintenance + digest sender
internal/api/                 chi router, handlers, middleware (CSRF, rate limit, auth context)
internal/auth/                argon2id passwords, securecookie sessions, WebAuthn (passkeys), RequireAuth/Admin middleware
internal/config/              env-var loading (typed Config)
internal/db/                  SQLite open, pragmas, embedded migrations (goose)
internal/digest/              SMTP daily-digest builder + sender (multipart/alt + STARTTLS)
internal/feed/                gofeed wrapper + readability fallback fetcher + Discover (homepage → feed URL)
internal/filters/             matcher (field/op/value), apply outcome combiner
internal/models/              data types shared across packages
internal/opml/                OPML import + export + discovery → subscribe
internal/poller/              adaptive scheduler, fetch dispatch, summary queue
internal/store/               SQLite CRUD, FTS5 search, app_settings KV, dbops, passkeys, digests
internal/summarize/           Summarizer interface + Ollama implementation + noop for tests
internal/sysinfo/             host-detection (RAM/CPU/GPU) + model recommendation
internal/urlcheck/            SSRF block (scheme allowlist + private-IP refusal)
internal/web/                 embed.FS handler for the SPA
web/                          Svelte 5 (runes) source; built via Vite, copied to internal/web/dist
```

## Database schema

SQLite. Migrations in `internal/db/migrations/*.sql`, applied at startup. Key tables:

- `users` — argon2id-hashed passwords; admin flag.
- `sessions` — server-side rows backing cookies; pruned periodically.
- `feeds` — shared across users; URL-unique. Tracks etag/last-modified/error counters.
- `subscriptions` — `(user_id, feed_id)` with category, title override, muted flag, and `position` for drag-reorder.
- `categories` — user-scoped folders with color + position.
- `articles` — shared across users; per-feed dedup by `guid` and `content_hash`. Carries `cleaned_html` (AI ad-stripped).
- `article_state` — per-user read/star/later flags.
- `articles_fts` — FTS5 virtual table on title/text/author; kept in sync via triggers.
- `boards` + `board_articles` — user-curated collections.
- `filters` — rule store; engine in `internal/filters`.
- `shares` — user-to-user article shares.
- `app_settings` — global KV (active model, schedules, branding, tuning).
- `saved_searches` — persisted FTS queries.
- `article_tags` — per-user tags on individual articles.
- `user_digests` — per-user opt-in daily-digest config (view, hour/minute UTC, last-sent timestamp).
- `passkeys` — WebAuthn credentials (credential_id, public_key, sign_count, name, timestamps).
- `webauthn_sessions` — short-lived ceremony state for in-flight register/login flows; reaped after 5 min.

WAL mode, 64 MiB page cache, 256 MiB mmap, busy_timeout=5s. Single Go connection — writes are serialized (SQLite single-writer); reads are fast enough that the connection pool isn't the bottleneck at this scale.

## Request lifecycle

```
HTTPS                Caddy
  └─> :443  ──tls──> ember:8080
                       │
                       ▼
                  chi.Router
                       │
        ┌──────────────┼──────────────┐
        ▼              ▼              ▼
   /api/auth/*    /api/*         /fever  (Fever shim)
                       │              │
                  CSRFVerify          │
                  RequireAuth         │
                  (RequireAdmin)      │
                       │              │
                       ▼              ▼
                   handlers       fever_handlers
                       │              │
                       └─> store ─────┘
                              │
                              ▼
                          SQLite (WAL)
```

Browsers store a server-side session cookie + a CSRF cookie. State-changing routes (`POST/PATCH/DELETE`) must echo the CSRF cookie in `X-Ember-CSRF`.

## Poller state machine

Each feed has `next_fetch` (unix seconds) and an `error_count`. The poller:

1. Every `EMBER_POLL_TICK` (default 60s): `Tick(ctx)` queries `FeedsDue` (next_fetch ≤ now).
2. Due feeds fan out across `EMBER_POLL_CONCURRENCY` worker goroutines.
3. Each worker calls `Fetcher.Fetch(url, etag, last_modified)`:
   - 304 → bookkeep last_fetched/next_fetch, error_count = 0.
   - 2xx → parse with gofeed, optionally enrich short bodies via go-readability, upsert articles.
   - Error → increment error_count, schedule next try with exponential backoff (capped at `MaxInterval`).
4. Newly-inserted articles enqueue on `summaryCh` (best-effort, drops on full).
5. Filters apply per-subscriber as articles land.

The summary worker is a separate goroutine that drains `summaryCh`, calls Ollama, writes `summary` + `summary_model` + `cleaned_html`. Failures stamp `summary_model = 'skipped'` so the article still surfaces in the UI.

Restart safety: `enqueuePendingSummaries` runs at startup and seeds the channel from any articles with empty `summary_model`.

## Summarizer pipeline

```
poller.summarize ─> Summarizer.Summarize(title, text)
                       │
                       ├─ Ollama: POST /api/generate
                       │   prompt = labeled "SUMMARY: / POINTS: / CLEANED:"
                       │   options = {temperature, top_p, num_ctx} from app_settings
                       │   timeout = 90s, one retry on transient error
                       │
                       └─> parseResult(s):
                              parseLabeled → parseJSONObject → parseJSONArray
                              → line-based fallback
                              + cleanBullets (strip markers / inline markdown /
                                              prompt-echo / label prefixes)
                              + stripEmphasis (** __ * ` " ')
                              + cleanParagraph
```

Active model + tunables held in `atomic.Value`/`atomic.Pointer` on the `Ollama` struct so the admin API can swap them without restarting.

## SPA

- Svelte 5 with runes (`$state`, `$derived`, `$effect`, `$props`).
- Vite 5 build, output copied to `internal/web/dist`, served via `embed.FS`.
- Typed fetch client in `web/src/lib/api.ts` (throws `ApiError`).
- Stores in `web/src/lib/stores.ts` for user, feeds, categories, boards, articles, themes, branding, new-article counter, etc.
- 15s auto-refresh poll while the tab is visible; SSE not used — REST polling is simpler and fits the cadence.
- Service worker (`web/public/sw.js`) caches assets immutably and falls back to cached shell when offline.

## Admin endpoints (admin-only)

- `GET /api/admin/llm` — detected hardware, recommendation, installed models, current model + options.
- `POST /api/admin/llm/model` / `…/pull` / `…/delete` / `…/options` — switch / pull / delete / tune.
- `GET /api/branding` (public) / `POST /api/admin/branding` (admin).
- `GET /api/admin/db` — size, page count, recent backups, schedules.
- `POST /api/admin/db/backup` / `…/cleanup` / `…/schedule` — manual + scheduled maintenance.
- `POST /api/feeds/resummarize-all` — re-process every article after a prompt change.
- `GET /api/admin/session` / `POST /api/admin/session/ttl` — server-wide session cookie lifetime.
- `GET /api/admin/settings` / `PATCH /api/admin/settings` — SMTP relay config + initial-backlog window. Overlays env-derived defaults at runtime; digest sender re-resolves every tick.
- `POST /api/admin/settings/email-test` — send a one-off diagnostic message through the live SMTP config.

Auth-required (not admin-only):

- `POST /api/articles/{id}/extract` — on-demand readability re-run for the reader pane's "Re-extract" button. Subject to the same SSRF check as the poller's automatic enrichment.

## E2E

`web/e2e/*.spec.ts` (Playwright). The binary in `EMBER_TEST_MODE=1` seeds a deterministic admin + 1 feed + 12 articles with known content, so specs can assert on titles/excerpts directly. Axe-core runs against the major screens for WCAG 2.1 AA.
