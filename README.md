# Ember

Self-hosted RSS/Atom reader. Single Go binary that embeds a Svelte SPA, serves a JSON API, runs a background poller, stores everything in SQLite (FTS5), and summarizes articles with a small local LLM via Ollama. Everything runs in containers.

See `EMBER_BUILD_PLAN.md` for the full implementation specification.

## Status

Phase 0 — bootstrap complete. CI green. Subsequent phases add the database, auth, feed pipeline, summarizer, poller, HTTP API, Svelte UI, and containerization.

## Quickstart (dev)

```
make test          # go tests
make web-install   # install web deps (first time)
make web-test      # vitest
make build         # build ./bin/ember
EMBER_TEST_MODE=1 ./bin/ember
```

Full Compose stack arrives in Phase 9 (`deploy/docker-compose.yml`).

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
