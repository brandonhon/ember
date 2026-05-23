# Ember — Self-Hosted RSS Reader: Build Plan

> **For Claude Code.** This is an implementation specification. Work through it **phase by phase, in order**. Do not skip ahead. At the end of every phase there is an **Acceptance Gate** — all tests in that gate must pass before moving to the next phase. Commit after each task with the suggested commit message. If a decision is ambiguous, prefer the simplest option that satisfies the tests and leave a `// TODO(ember):` note.

---

## 0. Project Summary

Ember is a self-hosted RSS/Atom feed reader inspired by Kagi's **kite-public** UI and **Tiny Tiny RSS** features, with a **Feedly-style three-pane layout**. It ships as a **single Go binary** that embeds a **Svelte SPA**, serves a JSON API, runs a background feed **poller**, stores everything in **SQLite (with FTS5)**, and generates per-article summaries using a **small local LLM via Ollama**.

Everything runs in **containers** via Docker Compose. **Every component has tests.**

### Non-negotiable requirements (from the user)
- Kite-style article/story design, but a traditional reader feel with a Feedly-like sidebar (folders/categories).
- Add / remove / update **feeds**.
- Add / remove / update **categories** (folders).
- **Login required.** Multiple **users**, each with a private feed view (no cross-user overlap of read/star state).
- Ability to **share** individual articles/feeds between users.
- **Star** articles.
- **Full-text search.**
- **Small LLM** doing summarization, fully local/self-hosted.
- **Scroll-to-mark-read** behavior.
- **Poller** that fetches new articles on a schedule.
- A "**Fresh**" view that shows only recent articles.
- Built in **Go**, served as an **SPA**, easy to deploy.
- **Runs in containers.**
- **Tests for everything.**

### Feedly-derived features to include (researched)
- Smart views: **Today**, **Fresh**, **All Unread**, **Starred**, **Read Later**, **Shared with me**.
- **Boards** (curated collections, like Feedly boards / TT-RSS labels).
- **Read Later** queue.
- **Recently Read** list.
- Mark-all-read (per feed / per folder / global).
- Card vs Compact density.
- Keyboard shortcuts (`j/k/m/s/r/o/?`).
- Feed auto-discovery from a site URL.
- OPML import/export.
- Mute/keyword **filters** (TT-RSS filter engine + Feedly's mute concept).

### Explicitly out of scope (v1)
- Mobile native apps (the SPA is a PWA instead).
- Federated/distributed scaling (single SQLite instance is fine for a household/small team).
- Annotations/highlights (deferred to a later phase; noted but not built in v1).
- Enterprise threat-intel graph.

---

## 1. Architecture

```
                         ┌──────────────────────────────────────────┐
                         │            ember (single Go binary)        │
                         │                                            │
   Browser ──HTTPS──▶ Caddy ──▶  net/http (chi router)                │
                         │        ├─ /api/*      JSON API             │
                         │        ├─ /fever       Fever API shim      │
                         │        └─ /*           embedded Svelte SPA │
                         │                                            │
                         │        ┌─ poller goroutine pool ──┐        │
                         │        │   gofeed + conditional GET │       │
                         │        │   go-readability (full text)│      │
                         │        └─ summarize via Ollama HTTP ─┘      │
                         │                                            │
                         │        SQLite (modernc, pure-Go) + FTS5    │
                         └──────────────────────────────────────────┘
                                          │  HTTP
                                          ▼
                                   ┌──────────────┐
                                   │    ollama    │  qwen2.5:1.5b
                                   │  (container) │  (CPU-friendly)
                                   └──────────────┘
```

### Container topology (Docker Compose)
| Service | Image | Purpose | Network |
|---|---|---|---|
| `caddy`   | `caddy:2-alpine`        | TLS termination + reverse proxy | `frontend` |
| `ember`   | built from repo         | API + SPA + poller + SQLite     | `frontend`, `backend` |
| `ollama`  | `ollama/ollama`         | local LLM summarizer            | `backend` (internal-only) |

- `ember` and `ollama` sit on an **internal-only** `backend` network. Only `caddy` is exposed publicly. (This avoids the egress/networking pitfall of restricting a container to internal-only when it actually needs to reach a sibling — `ember` reaches `ollama` over the shared `backend` net, and `ollama` needs no egress.)
- SQLite DB persisted on a named volume `ember-data`. Ollama models persisted on `ollama-models`.

### Decided tech choices (do not deviate without a test-backed reason)
- **Go 1.23+**, modules.
- **SQLite driver:** `modernc.org/sqlite` (pure-Go, CGO-free → trivial static cross-compile and `scratch`/`distroless` images). Must enable FTS5 (modernc builds it in).
- **Router:** `github.com/go-chi/chi/v5`.
- **Feed parsing:** `github.com/mmcdole/gofeed`.
- **Readability/full-text extraction:** `github.com/go-shiori/go-readability`.
- **Password hashing:** `golang.org/x/crypto/argon2` (argon2id).
- **Sessions:** signed, HttpOnly, SameSite=Lax cookies. Use `github.com/gorilla/securecookie` OR a small custom HMAC token. Prefer securecookie.
- **Migrations:** `github.com/pressly/goose/v3` (embedded SQL migrations).
- **Frontend:** **Svelte + Vite + TypeScript**, built to static assets, embedded via `embed.FS`. Reuse kite-public component styling where practical, but Ember owns its own components.
- **LLM client:** plain `net/http` calls to Ollama's `/api/generate` (or `/api/chat`). Behind a `Summarizer` interface so it can be swapped for an OpenAI-compatible endpoint.
- **Logging:** `log/slog` (stdlib structured logging).
- **Config:** environment variables, parsed once into a `Config` struct.

---

## 2. Repository Layout

Create exactly this structure:

```
ember/
├── cmd/
│   └── ember/
│       └── main.go                 # entrypoint: wires config, db, server, poller
├── internal/
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── db/
│   │   ├── db.go                   # open SQLite, set PRAGMAs, run migrations
│   │   ├── db_test.go
│   │   └── migrations/             # goose .sql files (embedded)
│   │       ├── 0001_init.sql
│   │       ├── 0002_fts.sql
│   │       └── ...
│   ├── models/
│   │   └── models.go               # structs: User, Feed, Category, Article, ArticleState, Board, Filter, Share, Session
│   ├── store/                      # data-access layer (one file per aggregate)
│   │   ├── store.go                # Store struct wrapping *sql.DB
│   │   ├── users.go      + users_test.go
│   │   ├── feeds.go      + feeds_test.go
│   │   ├── categories.go + categories_test.go
│   │   ├── articles.go   + articles_test.go
│   │   ├── state.go      + state_test.go      # per-user read/star/later
│   │   ├── boards.go     + boards_test.go
│   │   ├── filters.go    + filters_test.go
│   │   ├── shares.go     + shares_test.go
│   │   └── search.go     + search_test.go     # FTS5 queries
│   ├── auth/
│   │   ├── auth.go                 # hashing, session create/verify, middleware
│   │   └── auth_test.go
│   ├── feed/
│   │   ├── discover.go             # find RSS link from a site URL
│   │   ├── discover_test.go
│   │   ├── parse.go                # gofeed wrapper → normalized Article
│   │   ├── parse_test.go
│   │   ├── fetch.go                # conditional GET (ETag/Last-Modified), backoff
│   │   ├── fetch_test.go
│   │   ├── readability.go          # full-content extraction
│   │   └── readability_test.go
│   ├── poller/
│   │   ├── poller.go               # scheduler + worker pool + adaptive intervals
│   │   ├── poller_test.go
│   │   └── interval.go + interval_test.go
│   ├── summarize/
│   │   ├── summarize.go            # Summarizer interface
│   │   ├── ollama.go               # Ollama implementation
│   │   ├── ollama_test.go          # against httptest fake
│   │   └── noop.go                 # NoopSummarizer for tests/dev
│   ├── opml/
│   │   ├── opml.go                 # import/export
│   │   └── opml_test.go
│   ├── api/
│   │   ├── server.go               # chi setup, middleware, static embed mount
│   │   ├── server_test.go
│   │   ├── auth_handlers.go    + auth_handlers_test.go
│   │   ├── feed_handlers.go    + feed_handlers_test.go
│   │   ├── category_handlers.go+ category_handlers_test.go
│   │   ├── article_handlers.go + article_handlers_test.go
│   │   ├── board_handlers.go   + board_handlers_test.go
│   │   ├── share_handlers.go   + share_handlers_test.go
│   │   ├── search_handlers.go  + search_handlers_test.go
│   │   ├── user_handlers.go    + user_handlers_test.go
│   │   ├── fever.go            + fever_test.go     # Fever API shim (mobile clients)
│   │   ├── middleware.go
│   │   └── render.go               # JSON helpers, error envelope
│   └── web/
│       ├── embed.go                # //go:embed dist/* ; serves SPA, SPA fallback
│       └── embed_test.go
├── web/                            # Svelte frontend source
│   ├── package.json
│   ├── vite.config.ts
│   ├── tsconfig.json
│   ├── svelte.config.js
│   ├── playwright.config.ts
│   ├── index.html
│   ├── src/
│   │   ├── main.ts
│   │   ├── App.svelte
│   │   ├── lib/
│   │   │   ├── api.ts              # typed fetch client
│   │   │   ├── api.test.ts
│   │   │   ├── stores.ts           # svelte stores (auth, feeds, articles, ui)
│   │   │   ├── stores.test.ts
│   │   │   ├── keyboard.ts         # shortcut handling
│   │   │   ├── keyboard.test.ts
│   │   │   └── types.ts
│   │   ├── components/
│   │   │   ├── Login.svelte
│   │   │   ├── Sidebar.svelte
│   │   │   ├── FolderTree.svelte
│   │   │   ├── ArticleList.svelte
│   │   │   ├── StoryCard.svelte
│   │   │   ├── Reader.svelte
│   │   │   ├── SummaryCard.svelte
│   │   │   ├── TopBar.svelte
│   │   │   ├── SearchBar.svelte
│   │   │   ├── AddFeedModal.svelte
│   │   │   ├── ManageUsersModal.svelte
│   │   │   ├── ShareModal.svelte
│   │   │   └── Toast.svelte
│   │   └── components/__tests__/   # vitest component tests
│   └── e2e/                        # Playwright end-to-end specs
│       ├── auth.spec.ts
│       ├── feeds.spec.ts
│       ├── reading.spec.ts
│       └── search.spec.ts
├── deploy/
│   ├── docker-compose.yml
│   ├── docker-compose.dev.yml      # hot-reload overrides
│   ├── Caddyfile
│   └── .env.example
├── Dockerfile                      # multi-stage: build web → build go → distroless
├── Makefile
├── .github/
│   └── workflows/
│       └── ci.yml                  # lint + test (go + web) + docker build
├── .gitignore
├── .dockerignore
├── go.mod
├── README.md
└── EMBER_BUILD_PLAN.md             # this file
```

---

## 3. Data Model (SQLite)

Migration `0001_init.sql` creates the core schema. Key principles:
- **Articles are shared storage**, deduplicated across users who follow the same feed.
- **Per-user state is separate** (`article_state`) so read/star/later never overlaps between users.
- Use `INTEGER PRIMARY KEY` (rowid) everywhere; store timestamps as Unix epoch `INTEGER`.
- Enable `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;` on every connection.

```sql
-- 0001_init.sql  (goose Up/Down)

CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE,
  email         TEXT,
  password_hash TEXT NOT NULL,
  is_admin      INTEGER NOT NULL DEFAULT 0,
  settings_json TEXT NOT NULL DEFAULT '{}',
  created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,          -- random 32-byte hex
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  user_agent TEXT
);

CREATE TABLE feeds (
  id             INTEGER PRIMARY KEY,
  url            TEXT NOT NULL,          -- the feed URL
  site_url       TEXT,
  title          TEXT NOT NULL,
  favicon_url    TEXT,
  etag           TEXT,
  last_modified  TEXT,
  last_fetched   INTEGER,
  next_fetch     INTEGER,                -- adaptive scheduling
  fetch_interval INTEGER NOT NULL DEFAULT 1800,  -- seconds
  error_count    INTEGER NOT NULL DEFAULT 0,
  last_error     TEXT,
  created_at     INTEGER NOT NULL,
  UNIQUE(url)
);

CREATE TABLE categories (
  id        INTEGER PRIMARY KEY,
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name      TEXT NOT NULL,
  color     TEXT,
  position  INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

-- A user subscribes to a feed, optionally filed under a category.
-- This is what makes feeds per-user. Two users can subscribe to the
-- same feed row but have different categories and state.
CREATE TABLE subscriptions (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  feed_id     INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL,
  title_override TEXT,
  created_at  INTEGER NOT NULL,
  UNIQUE(user_id, feed_id)
);

CREATE TABLE articles (
  id           INTEGER PRIMARY KEY,
  feed_id      INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  guid         TEXT NOT NULL,
  url          TEXT,
  title        TEXT NOT NULL,
  author       TEXT,
  content_html TEXT,
  content_text TEXT,                     -- stripped, for FTS + summarizer
  summary      TEXT,                     -- LLM output (JSON array of bullets)
  summary_model TEXT,
  image_url    TEXT,
  published_at INTEGER,
  fetched_at   INTEGER NOT NULL,
  content_hash TEXT NOT NULL,            -- dedup: hash(url|title|content)
  UNIQUE(feed_id, guid)
);
CREATE INDEX idx_articles_feed_pub ON articles(feed_id, published_at DESC);
CREATE INDEX idx_articles_hash ON articles(content_hash);

CREATE TABLE article_state (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  is_read    INTEGER NOT NULL DEFAULT 0,
  is_starred INTEGER NOT NULL DEFAULT 0,
  is_later   INTEGER NOT NULL DEFAULT 0,
  read_at    INTEGER,
  starred_at INTEGER,
  PRIMARY KEY (user_id, article_id)
);
CREATE INDEX idx_state_user_star ON article_state(user_id, is_starred);
CREATE INDEX idx_state_user_later ON article_state(user_id, is_later);

CREATE TABLE boards (
  id        INTEGER PRIMARY KEY,
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name      TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE board_articles (
  board_id   INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  added_at   INTEGER NOT NULL,
  PRIMARY KEY (board_id, article_id)
);

CREATE TABLE filters (
  id        INTEGER PRIMARY KEY,
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name      TEXT NOT NULL,
  match_json TEXT NOT NULL,   -- {"field":"title","op":"contains","value":"crypto"}
  action    TEXT NOT NULL,    -- "mark_read" | "star" | "hide" | "tag"
  enabled   INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);

CREATE TABLE shares (
  id          INTEGER PRIMARY KEY,
  article_id  INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  from_user   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  to_user     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  note        TEXT,
  created_at  INTEGER NOT NULL,
  seen        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_shares_to ON shares(to_user, seen);
```

`0002_fts.sql` adds full-text search:

```sql
-- FTS5 virtual table mirroring article text, kept in sync via triggers.
CREATE VIRTUAL TABLE articles_fts USING fts5(
  title, content_text, author,
  content='articles', content_rowid='id',
  tokenize = 'porter unicode61'
);

CREATE TRIGGER articles_ai AFTER INSERT ON articles BEGIN
  INSERT INTO articles_fts(rowid, title, content_text, author)
  VALUES (new.id, new.title, new.content_text, new.author);
END;
CREATE TRIGGER articles_ad AFTER DELETE ON articles BEGIN
  INSERT INTO articles_fts(articles_fts, rowid, title, content_text, author)
  VALUES ('delete', old.id, old.title, old.content_text, old.author);
END;
CREATE TRIGGER articles_au AFTER UPDATE ON articles BEGIN
  INSERT INTO articles_fts(articles_fts, rowid, title, content_text, author)
  VALUES ('delete', old.id, old.title, old.content_text, old.author);
  INSERT INTO articles_fts(rowid, title, content_text, author)
  VALUES (new.id, new.title, new.content_text, new.author);
END;
```

---

## 4. API Surface

All under `/api`. JSON in/out. Auth via session cookie. Standard error envelope: `{"error":{"code":"...","message":"..."}}`. Successful list endpoints return `{"data":[...],"meta":{...}}`.

### Auth
- `POST /api/auth/login` `{username,password}` → sets cookie, returns user.
- `POST /api/auth/logout`
- `GET  /api/me` → current user + settings.

### Users (admin only for create/delete)
- `GET    /api/users`
- `POST   /api/users` `{username,email,password,is_admin}`
- `PATCH  /api/users/:id`
- `DELETE /api/users/:id`
- `PATCH  /api/me/settings` `{settings_json}`

### Categories
- `GET    /api/categories`
- `POST   /api/categories` `{name,color}`
- `PATCH  /api/categories/:id` `{name,color,position}`
- `DELETE /api/categories/:id` (feeds become uncategorized)

### Feeds / Subscriptions
- `GET    /api/feeds` → user's subscriptions with unread counts + category.
- `POST   /api/feeds` `{url, category_id?}` → auto-discovers feed link, dedups into `feeds`, creates subscription. Triggers an immediate poll of that feed.
- `PATCH  /api/feeds/:id` `{title_override?, category_id?, fetch_interval?}`
- `DELETE /api/feeds/:id` → removes subscription (feed row kept if others subscribe).
- `POST   /api/feeds/:id/refresh` → poll now.
- `POST   /api/feeds/import` (multipart OPML) → bulk subscribe.
- `GET    /api/feeds/export` → OPML download.

### Articles
- `GET /api/articles?view=&feed_id=&category_id=&board_id=&unread=&starred=&later=&fresh=&q=&cursor=&limit=`
  - `view` ∈ `today|fresh|unread|starred|later|shared`.
  - `fresh=1` filters to `published_at >= now - FRESH_WINDOW`.
  - Cursor pagination (keyset on `published_at,id`).
- `GET  /api/articles/:id` → full article incl. content + summary.
- `POST /api/articles/read`   `{ids:[],read:true}` (bulk).
- `POST /api/articles/star`   `{id,star:true}`.
- `POST /api/articles/later`  `{id,later:true}`.
- `POST /api/articles/mark-all-read` `{feed_id?|category_id?|view?}`.

### Boards
- `GET    /api/boards`
- `POST   /api/boards` `{name}`
- `DELETE /api/boards/:id`
- `POST   /api/boards/:id/articles` `{article_id}`
- `DELETE /api/boards/:id/articles/:articleId`

### Filters
- `GET/POST/PATCH/DELETE /api/filters[...]`

### Shares
- `POST /api/shares` `{article_id, to_user, note?}`
- `GET  /api/shares/inbox` → "Shared with me".
- `POST /api/shares/:id/seen`

### Search
- `GET /api/search?q=&limit=&cursor=` → FTS5 ranked results scoped to the user's subscriptions.

### Fever API shim (for mobile clients)
- `POST /fever` implementing the documented Fever endpoints (`api`, `feeds`, `groups`, `items`, `unread_item_ids`, `saved_item_ids`, `mark`). Auth via Fever's `api_key = md5(user:password)`. This unlocks Reeder/FeedMe/etc. without writing a mobile app.

---

## 5. Phased Implementation

> Each task lists: **files**, **what to build**, **tests required**, **commit message**. A phase is done only when its **Acceptance Gate** passes.

---

### PHASE 0 — Bootstrap & CI
**Goal:** compiling skeleton, tooling, and a green CI before any features.

**Tasks**
1. `go mod init github.com/<you>/ember`. Add the Makefile with targets: `build`, `test`, `lint`, `web-build`, `web-test`, `e2e`, `docker`, `run`, `cover`.
2. `cmd/ember/main.go` prints version and exits 0. `internal/config/config.go` parses env (`EMBER_ADDR`, `EMBER_DB_PATH`, `EMBER_SESSION_KEY`, `EMBER_OLLAMA_URL`, `EMBER_OLLAMA_MODEL`, `EMBER_FRESH_WINDOW`, `EMBER_LOG_LEVEL`) with sane defaults.
3. Set up `golangci-lint` config and `.gitignore`/`.dockerignore`.
4. `.github/workflows/ci.yml`: matrix runs `make lint test web-test`, then `make docker`.
5. Scaffold the Svelte app with Vite + TS + Vitest + Playwright (`npm create vite`, choose svelte-ts). Add a trivial `App.svelte` rendering "Ember".

**Tests required**
- `config_test.go`: defaults applied; env overrides parsed; invalid values rejected.
- `web/src/lib/__smoke__`: a Vitest test asserting the app mounts.
- CI must be green on a clean checkout.

**Commit:** `chore: bootstrap go module, svelte app, makefile, CI`

**Acceptance Gate 0:** `make lint test web-test` exits 0 locally and in CI. `make build` produces a binary. `make web-build` produces `web/dist`.

---

### PHASE 1 — Database & Store Layer
**Goal:** schema, migrations, and a fully tested data-access layer. No HTTP yet.

**Tasks**
1. `internal/db/db.go`: open SQLite via modernc, apply PRAGMAs, run embedded goose migrations on startup. Provide `OpenTest(t)` helper returning an in-memory or temp-file DB with migrations applied.
2. Write migrations `0001_init.sql` and `0002_fts.sql` (Section 3).
3. `internal/models/models.go`: all structs with JSON tags.
4. Implement each `store/*.go` file with CRUD + the specific queries the API needs (unread counts per feed/category, keyset-paginated article queries, FTS search, share inbox, etc.).
5. Implement dedup in `articles.go`: `UpsertArticle` skips when `content_hash` already exists for the feed.

**Tests required (this is the most important test surface — be thorough)**
- `db_test.go`: migrations apply cleanly up and down; PRAGMAs set; FTS table + triggers exist.
- `users_test.go`: create/get/list/update/delete; unique username enforced.
- `feeds_test.go`: insert/dedup by URL; subscription per-user; deleting a subscription keeps the feed if others reference it.
- `categories_test.go`: CRUD; deleting a category nulls subscriptions' category_id.
- `articles_test.go`: upsert + dedup (same hash inserted twice → one row); keyset pagination returns stable ordering; unread counts correct.
- `state_test.go`: **two users, same article, independent read/star/later state** (explicit cross-user isolation test).
- `boards_test.go`, `filters_test.go`, `shares_test.go`: CRUD + the inbox query.
- `search_test.go`: insert articles, search returns ranked hits, results scoped to the querying user's subscriptions only.
- Aim for **>85% coverage** in `internal/store`.

**Commit:** `feat(store): sqlite schema, migrations, fully-tested data layer`

**Acceptance Gate 1:** `go test ./internal/db/... ./internal/store/...` passes; coverage report shows store ≥85%. The cross-user isolation test and the dedup test must exist and pass.

---

### PHASE 2 — Auth
**Goal:** registration (admin-seeded), login, sessions, middleware.

**Tasks**
1. `auth/auth.go`: `HashPassword`/`VerifyPassword` (argon2id, tunable params from config); `CreateSession`/`VerifySession`/`DestroySession`; `RequireAuth` and `RequireAdmin` chi middleware reading the session cookie.
2. First-run bootstrap: if `users` is empty, create an admin from `EMBER_ADMIN_USER`/`EMBER_ADMIN_PASSWORD` (log a warning if defaults used).
3. Secure cookie via `gorilla/securecookie` keyed by `EMBER_SESSION_KEY`.

**Tests required**
- `auth_test.go`: hash≠plaintext; verify true/false paths; argon2 params honored; session create→verify→expire→destroy; tampered cookie rejected; `RequireAuth` returns 401 without/with-bad cookie and passes with good cookie; `RequireAdmin` 403 for non-admin.

**Commit:** `feat(auth): argon2id passwords, secure-cookie sessions, middleware`

**Acceptance Gate 2:** `go test ./internal/auth/...` passes. Tampered/expired sessions provably rejected.

---

### PHASE 3 — Feed Fetching & Parsing
**Goal:** turn a URL into normalized articles, with conditional GET and full-text extraction. All tested against local fixtures/httptest — **no live network in tests.**

**Tasks**
1. `feed/discover.go`: given a site URL, fetch HTML and find `<link rel="alternate" type="application/rss+xml|atom+xml">`; fall back to common paths (`/feed`, `/rss`, `/atom.xml`). Return the feed URL.
2. `feed/fetch.go`: HTTP client with timeout, custom User-Agent, conditional GET (send stored ETag/Last-Modified, handle 304), and exponential backoff bookkeeping (returns whether content changed).
3. `feed/parse.go`: wrap gofeed; normalize each item to an `Article` (resolve relative URLs, pick best date, strip tracking params, compute `content_hash`, derive `content_text` from HTML, extract first image).
4. `feed/readability.go`: optional full-content fetch+extract for excerpt-only feeds, via go-readability. Respect a per-feed toggle.

**Tests required**
- Save **fixture files** under `feed/testdata/` (sample RSS 2.0, Atom, an HTML page with a discoverable feed link, an HTML article for readability). Commit these.
- `discover_test.go`: finds the link in the fixture; fallback paths; returns error when none.
- `fetch_test.go`: use `httptest.Server` to assert conditional headers sent, 304 handled (no re-parse), 200 returns body, timeouts/5xx surface errors and bump backoff.
- `parse_test.go`: RSS and Atom fixtures parse to expected normalized fields; same item parsed twice → identical `content_hash`; relative links resolved; date fallbacks.
- `readability_test.go`: HTML article fixture → extracted text contains expected sentence, boilerplate removed.

**Commit:** `feat(feed): discovery, conditional fetch, parsing, readability — fixture-tested`

**Acceptance Gate 3:** `go test ./internal/feed/...` passes offline (no network). Conditional-GET 304 path and hash-stability test must pass.

---

### PHASE 4 — Summarizer (small LLM)
**Goal:** local summarization via Ollama, behind an interface, tested without a real model.

**Tasks**
1. `summarize/summarize.go`: `type Summarizer interface { Summarize(ctx, title, text string) ([]string, model string, err error) }`. Returns 3–5 bullet strings.
2. `summarize/ollama.go`: POST to `${EMBER_OLLAMA_URL}/api/generate` with a tight prompt instructing: neutral tone, 3 bullets, no preamble, JSON array output; parse the JSON array; truncate input to a token budget; context timeout; one retry.
3. `summarize/noop.go`: returns a deterministic fake (used in dev and in poller tests).
4. Wire model + URL from config. Default model `qwen2.5:1.5b` (document `llama3.2:1b` as a lighter alt).

**Tests required**
- `ollama_test.go`: spin an `httptest.Server` that emulates Ollama's response shape; assert the request prompt/body is well-formed, JSON parsing works, malformed model output falls back gracefully (returns error, not panic), context cancellation respected.
- `noop` covered by a trivial determinism test.

**Commit:** `feat(summarize): ollama summarizer behind interface, httptest-covered`

**Acceptance Gate 4:** `go test ./internal/summarize/...` passes with no real Ollama running.

---

### PHASE 5 — Poller
**Goal:** background scheduler that fetches due feeds concurrently, stores new articles, applies filters, and enqueues summaries.

**Tasks**
1. `poller/interval.go`: adaptive interval calc — frequent posters polled sooner, idle/erroring feeds backed off (cap min 5 min, max 6 h). Pure function, easily testable.
2. `poller/poller.go`: ticker selects feeds where `next_fetch <= now`; bounded worker pool (`EMBER_POLL_CONCURRENCY`); for each: fetch→parse→upsert new articles→apply user filters→enqueue summary jobs (bounded queue, summaries best-effort/async so a slow LLM never blocks ingestion). Update `last_fetched`, `next_fetch`, error counters. Structured logs + simple in-memory metrics (counts).
3. Graceful shutdown via context.
4. Expose `RefreshFeed(ctx, feedID)` for the on-demand API endpoint.

**Tests required**
- `interval_test.go`: table-driven — verify intervals for high-frequency, low-frequency, and erroring feeds; clamping at min/max.
- `poller_test.go`: inject a **fake fetcher** (returns fixture feed) and the `noop` summarizer; run one tick against a temp DB; assert new articles inserted, dedup on a second tick (no duplicates), `next_fetch` advanced, error path increments `error_count` and backs off, context cancellation stops workers. No real network, no real LLM.

**Commit:** `feat(poller): adaptive scheduler, worker pool, async summaries — tested`

**Acceptance Gate 5:** `go test ./internal/poller/...` passes; second-tick dedup and backoff tests pass.

---

### PHASE 6 — HTTP API
**Goal:** wire store + auth + poller + summarizer into the chi router. Every handler tested.

**Tasks**
1. `api/server.go`: chi router, middleware chain (recoverer, request-id, slog request logging, gzip, auth where needed), mount `/api`, mount Fever at `/fever`, mount embedded SPA last with SPA fallback (unknown non-`/api` routes → `index.html`).
2. `api/render.go`: JSON success/error helpers, consistent envelope, cursor encoding.
3. Implement all handler files per Section 4. Enforce ownership everywhere (a user can only touch their own subscriptions/state/boards/shares; admin-gate user management).
4. `opml/opml.go`: parse/generate OPML; wire into import/export handlers.
5. `api/fever.go`: implement the Fever endpoints mapping to the store.

**Tests required (httptest against a real temp DB, fake fetcher/summarizer)**
- One `_test.go` per handler file. For each endpoint test: happy path, unauthorized (401), forbidden/cross-user (403/404), validation errors (400), and the core behavior assertion.
- **Critical cross-user test**: user A cannot read/modify user B's article state, subscriptions, boards, or shares; search results never leak B's private feeds to A.
- `article_handlers_test.go`: `view=fresh` only returns items within the window; cursor pagination returns each item exactly once across pages; mark-all-read scoping.
- `share_handlers_test.go`: A shares to B → appears in B's inbox, not A's; B marking seen.
- `fever_test.go`: auth via md5 key; `items`/`unread_item_ids`/`mark` behave per spec against fixtures.
- `server_test.go`: SPA fallback serves index.html for unknown routes but 404s `/api/nope`.

**Commit:** `feat(api): full REST + Fever shim + OPML, handler tests with cross-user isolation`

**Acceptance Gate 6:** `go test ./internal/api/... ./internal/opml/...` passes. Cross-user isolation tests are present and green. Coverage of `internal/api` ≥80%.

---

### PHASE 7 — Frontend SPA
**Goal:** the Svelte three-pane reader matching the mockup, wired to the API.

> Use the provided HTML mockup (`ember-reader.html`) as the visual source of truth: editorial/newsprint aesthetic, Fraunces + Newsreader fonts, ember accent, light/dark. Reproduce its three-pane layout, story cards, summary card, sidebar folder tree, and login screen as real Svelte components.

**Tasks**
1. `lib/api.ts`: typed client for every endpoint; throws typed errors; handles 401 by redirecting to login.
2. `lib/stores.ts`: auth store, feed/category tree store, article-list store (with cursor pagination + filters), UI store (theme, density, active view).
3. `lib/keyboard.ts`: `j/k` next/prev, `o` open original, `m` toggle read, `s` star, `r` refresh, `/` focus search, `?` shortcut overlay.
4. Build components: `Login`, `TopBar`, `SearchBar`, `Sidebar`+`FolderTree`, `ArticleList`+`StoryCard`, `Reader`+`SummaryCard`, `AddFeedModal`, `ManageUsersModal`, `ShareModal`, `Toast`.
5. **Scroll-to-mark-read**: in `ArticleList`, an IntersectionObserver marks a story read once it scrolls past the top threshold (debounced, batched into a single `POST /api/articles/read`). Respect a user setting to disable it.
6. **Fresh view** + "Fresh only" toggle hitting `fresh=1`.
7. PWA manifest + service worker (offline shell + cache last-read articles).

**Tests required**
- **Vitest unit/component tests** (`components/__tests__`): `StoryCard` renders read/unread/starred states and fires star/share/later events; `FolderTree` collapses and emits selection; `keyboard.test.ts` maps keys to actions; `api.test.ts` builds correct URLs/bodies and handles 401; `stores.test.ts` pagination + filter transitions.
- **Playwright e2e** (`web/e2e/`) run against the **real binary + a seeded temp DB** (use the `noop` summarizer and a fixture-backed fetcher via an env flag like `EMBER_TEST_MODE=1`):
  - `auth.spec.ts`: login required; bad creds rejected; logout.
  - `feeds.spec.ts`: add feed by URL (mocked discovery), it appears in sidebar; create/rename/delete category; move feed between categories.
  - `reading.spec.ts`: open article → summary card shows; scrolling list marks items read (badge decrements); star persists across reload; "Fresh only" filters list.
  - `search.spec.ts`: query returns expected article; results scoped to current user.

**Commit:** `feat(web): svelte three-pane reader, scroll-to-read, keyboard nav, PWA`

**Acceptance Gate 7:** `make web-test` (vitest) and `make e2e` (playwright) pass. The scroll-to-mark-read and star-persistence e2e assertions must pass.

---

### PHASE 8 — Embed & Single-Binary
**Goal:** fold the built SPA into the Go binary.

**Tasks**
1. `internal/web/embed.go`: `//go:embed all:dist` (the Vite build copied to `internal/web/dist` during build); serve assets with correct content-types and long cache headers for hashed assets; SPA fallback to `index.html`.
2. Makefile `build` target: `make web-build` → copy `web/dist` to `internal/web/dist` → `go build` produces one binary that serves the whole app.
3. `embed_test.go`: assert `index.html` and a known hashed asset are embedded and served; unknown route falls back to index; `/api/*` not shadowed.

**Commit:** `feat(web): embed SPA into binary via embed.FS`

**Acceptance Gate 8:** running the single binary serves the full working app at `EMBER_ADDR` with no external web server. `go test ./internal/web/...` passes.

---

### PHASE 9 — Containers & Compose
**Goal:** the whole thing runs with `docker compose up`.

**Tasks**
1. **`Dockerfile`** — multi-stage:
   - Stage `web`: `node:lts-alpine`, `npm ci && npm run build`.
   - Stage `build`: `golang:1.23`, copy web dist into `internal/web/dist`, `CGO_ENABLED=0 go build -ldflags "-s -w"` → static binary.
   - Final: `gcr.io/distroless/static` (or `scratch`), copy binary, non-root user, `EXPOSE`, healthcheck.
2. **`deploy/docker-compose.yml`**: services `caddy`, `ember`, `ollama` per Section 1; `frontend`/`backend` networks (backend `internal: true`); volumes `ember-data`, `ollama-models`; healthchecks; `ember` depends_on `ollama` healthy. An **init step** (compose `command` or a one-shot service) runs `ollama pull qwen2.5:1.5b` on first boot.
3. **`deploy/Caddyfile`**: reverse proxy `:443` → `ember:8080`, automatic HTTPS (or `tls internal` for homelab), gzip, security headers.
4. **`deploy/.env.example`** documenting every variable.
5. `docker-compose.dev.yml`: bind-mounts + `air`/`vite dev` for hot reload.
6. **README** quickstart: `cp deploy/.env.example .env && docker compose -f deploy/docker-compose.yml up -d`.

**Tests required**
- A **smoke test script** `deploy/smoke_test.sh` (run in CI via compose): brings the stack up, waits for health, `curl`s `/api/me` (expect 401), logs in, adds a fixture feed, asserts an article appears, hits `/api/search`. Tear down. CI job `compose-smoke` runs it.
- `Dockerfile` build is exercised by the `make docker` CI job.

**Commit:** `feat(deploy): multi-stage Dockerfile, compose stack, caddy, ollama, smoke test`

**Acceptance Gate 9:** `docker compose -f deploy/docker-compose.yml up` yields a working app reachable through Caddy; `deploy/smoke_test.sh` passes in CI; Ollama summarization works end-to-end on at least one real article (manual check documented in README).

---

### PHASE 10 — Hardening & Polish
**Goal:** production-readiness.

**Tasks**
1. Rate-limit auth endpoints; CSRF protection for cookie-auth state-changing requests (double-submit token or SameSite=strict + origin check); security headers.
2. Backups: documented `sqlite3 .backup` cron snippet + a `make backup` target; optional restic note.
3. Observability: `/healthz`, `/readyz`, basic Prometheus-style `/metrics` (poll counts, queue depth, errors).
4. Graceful shutdown across server + poller + summary queue.
5. Filters engine fully wired in poller + a user UI to manage them.
6. Accessibility pass on the SPA (focus management, ARIA on the three panes, keyboard-only navigation).

**Tests required**
- `middleware_test.go`: rate limiter trips after N attempts; CSRF rejects missing/mismatched token; security headers present.
- Health/ready/metrics endpoint tests.
- An a11y check in Playwright (axe) on the main views.

**Commit:** `feat: hardening — csrf, rate limit, health/metrics, filters UI, a11y`

**Acceptance Gate 10:** full test suite green: `make test web-test e2e` + `compose-smoke`. No `TODO(ember)` blocking core features.

---

## 6. Testing Strategy (summary)

| Layer | Tool | What it covers |
|---|---|---|
| Go unit | `go test` + table tests | config, store (incl. cross-user isolation, dedup), auth, feed parsing (fixtures), interval math, summarizer (httptest), opml |
| Go integration | `go test` + `httptest` + temp SQLite | every API handler, Fever shim, poller tick, SPA fallback |
| Frontend unit | Vitest | api client, stores, keyboard map, component states |
| E2E | Playwright vs real binary (`EMBER_TEST_MODE`) | login, add feed, scroll-to-read, star persistence, fresh filter, search |
| System | `deploy/smoke_test.sh` via Docker Compose in CI | full stack incl. Caddy + Ollama |

**Rules for Claude Code:**
- **No test may hit the live internet.** Use fixtures (`testdata/`), `httptest`, and `EMBER_TEST_MODE` fakes (fake fetcher + noop summarizer).
- Write the test in the **same commit** as the code it covers.
- Keep `internal/store` ≥85% and `internal/api` ≥80% line coverage; `make cover` prints the report and CI fails below threshold.
- Deterministic tests only — seed clocks/IDs where randomness would otherwise leak in (inject a `now func() time.Time`).

---

## 7. Environment Variables

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

---

## 8. Open Decisions (resolve early, note the choice in README)

1. **Single-user vs multi-user UI exposure** — the schema is multi-user from day one (required). Expose user management behind admin only.
2. **Pure-Go SQLite (`modernc`)** chosen over CGO `mattn` for static builds; if write throughput ever matters more than build simplicity, revisit.
3. **Summaries at ingest vs on-demand** — default ingest-time (async, best-effort) so reading is instant; fall back to on-demand if the model is slow on the target box.
4. **License** — Kite is MIT; pick AGPL if you want derivatives kept open, MIT otherwise. Keep any CC-BY-NC seed data out of the repo; ship empty and rely on OPML import.

---

## 9. Suggested Build Order Recap (for Claude Code)

```
Phase 0  bootstrap + CI            → Gate 0
Phase 1  db + store (+ tests)      → Gate 1   ← most test-critical
Phase 2  auth                      → Gate 2
Phase 3  feed fetch/parse          → Gate 3
Phase 4  summarizer                → Gate 4
Phase 5  poller                    → Gate 5
Phase 6  HTTP API + Fever + OPML   → Gate 6   ← cross-user isolation
Phase 7  Svelte SPA                → Gate 7
Phase 8  embed single-binary       → Gate 8
Phase 9  containers + compose      → Gate 9
Phase 10 hardening + a11y          → Gate 10
```

Work top to bottom. Do not begin a phase until the previous Acceptance Gate is green. Commit per task. Keep every test offline and deterministic.
