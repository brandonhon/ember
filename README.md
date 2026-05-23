# Ember

Self-hosted RSS/Atom reader. A single Go binary serving an embedded Svelte SPA, a JSON API + Fever shim, and a background poller that ingests feeds into SQLite (FTS5) and summarizes articles with a small local LLM via Ollama. Everything runs in containers.

See `EMBER_BUILD_PLAN.md` for the full implementation specification.

## Quickstart (Docker)

```
cd deploy
cp .env.example .env
# Edit .env — set EMBER_SESSION_KEY (32+ random bytes) and EMBER_ADMIN_PASSWORD
docker compose up -d
```

Then open `https://localhost` (Caddy serves the SPA + reverse-proxies the API). Log in with the admin credentials you set in `.env`.

On first boot:
1. The `ollama-pull` container fetches the configured model (default `qwen2.5:1.5b`).
2. The `ember` container creates the admin user from `EMBER_ADMIN_USER` / `EMBER_ADMIN_PASSWORD`.
3. The poller starts on a 60s tick (configurable via `EMBER_POLL_TICK`).

To verify the stack end-to-end, `deploy/smoke_test.sh` brings the stack up, logs in, hits a few endpoints, and tears down. CI runs this.

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

## Environment

| Var | Default | Purpose |
|---|---|---|
| `EMBER_ADDR` | `:8080` | listen address |
| `EMBER_DB_PATH` | `/data/ember.db` | SQLite file |
| `EMBER_SESSION_KEY` | _(required)_ | securecookie key (32+ bytes) |
| `EMBER_ADMIN_USER` | `admin` | first-run admin |
| `EMBER_ADMIN_PASSWORD` | _(required first run)_ | first-run admin password |
| `EMBER_OLLAMA_URL` | `http://ollama:11434` | summarizer endpoint |
| `EMBER_OLLAMA_MODEL` | `qwen2.5:1.5b` | model name |
| `EMBER_FRESH_WINDOW` | `6h` | "Fresh" cutoff |
| `EMBER_POLL_CONCURRENCY` | `8` | poller workers |
| `EMBER_POLL_TICK` | `60s` | scheduler tick |
| `EMBER_LOG_LEVEL` | `info` | slog level |
| `EMBER_TEST_MODE` | `0` | enables fake fetcher/summarizer for e2e |

## Mobile

Reeder, FeedMe, and other Fever-compatible clients can connect via the Fever shim at `/fever`. The `api_key` is `md5("<username>:<user_id>")` — see `/api/me` for your user_id. (We can't use the canonical `md5("user:pass")` because passwords are stored only as argon2id hashes.)

## Status

| Phase | Status |
|---|---|
| 0. Bootstrap + CI | ✅ |
| 1. DB + store | ✅ (≥85% coverage) |
| 2. Auth (argon2id + sessions) | ✅ |
| 3. Feed pipeline (discover/fetch/parse/readability) | ✅ |
| 4. Summarizer (Ollama + noop) | ✅ |
| 5. Poller (adaptive, async summaries) | ✅ |
| 6. HTTP API + Fever + OPML | ✅ |
| 7. Svelte SPA (three-pane + scroll-to-read + search) | ✅ minimum viable, no PWA yet |
| 8. Embed single-binary | ✅ |
| 9. Docker + Compose + Caddy | ✅ |
| 10. Hardening (CSRF, rate limit, health, a11y) | ✅ |
| 11. Playwright e2e (auth, feeds, reading + scroll-to-read, search) | ✅ |

## E2E

```
make embed build              # produce ./bin/ember with the SPA embedded
cd web && npx playwright install chromium
npx playwright test           # spawns the binary in test mode against a temp DB
```

In test mode (`EMBER_TEST_MODE=1`) the binary seeds a deterministic admin
(`admin` / `admintest`) plus 12 fixture articles and a single feed, so every
spec has known data to assert against. CI runs the suite on every push.

### Deferred to a follow-up
- Service worker / PWA offline shell.
- Filters engine UI (the schema and store-side query exist; UI is a TODO).
- Manage Users / Share modals; OPML upload UI (the API endpoint exists).
