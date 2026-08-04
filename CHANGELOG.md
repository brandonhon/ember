# Changelog

All notable user-facing changes to Ember are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Ember adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Per-tag [GitHub Releases](https://github.com/brandonhon/ember/releases) hold the
full commit-level list; this file curates the highlights and behavior changes.

From 0.9.6 onward, build-infrastructure dependency bumps (pinned GitHub Actions
and the like) are recorded too, under **Changed** and labelled as such. They
don't reach the released binaries or container image — they're listed so the
provenance of the build is auditable from one file. Releases before 0.9.6 omit
them.

## [Unreleased]

### Changed

- Bumped the pinned `actions/checkout` from 6.0.2 to 7.0.1 across all five
  workflows. This is build infrastructure only: it changes how CI checks the
  repository out, not anything in the released binaries or container image.
  Version 7 stops fork pull-request code being checked out under
  `pull_request_target` / `workflow_run`; Ember uses neither trigger, so the
  behaviour of every workflow is unchanged.

## [0.9.6] - 2026-08-04

### Security

- **Feed error messages no longer expose server network details.** When a feed
  fails to fetch, the reason shown in the sidebar tooltip is read by every
  subscriber of that feed. It was the raw Go error, which embeds whatever the
  connection resolved to — e.g. `dial tcp 10.0.0.5:443: connect: connection
  refused`, handing any user a piece of your internal network map. Subscribers
  now see a plain summary ("could not connect to the server", "the server
  responded 404", "the server's TLS certificate could not be verified"), while
  the complete error is still written to the server log for operators. The
  mapping is fail-closed: an unrecognised error reports a generic message
  rather than falling back to the raw text.
- Bumped `golang.org/x/text` to 0.40.0, picking up the fix for `GO-2026-5970`
  (an infinite loop on invalid input). Ember reached the affected code when
  draining an Ollama model-pull response.
- **Repeated failed logins are now throttled per account, not just per IP.**
  Ember already rate-limited the login endpoint per source address, which does
  nothing against a credential-stuffing run spread across many addresses. Each
  username now also gets an escalating backoff: the first five consecutive
  failures are free, after which every further attempt has to wait — 1s, 2s,
  4s, doubling up to a one-minute ceiling — returned as `429` with a
  `Retry-After` header. A successful sign-in clears it immediately. This is
  deliberately a delay and not a lockout: a hard lock would let anyone who
  knows your username keep you out of your own reader. Failed attempts past the
  free allowance are now logged with the username and client IP so an attack in
  progress is visible.
- **A passkey whose signature counter stops advancing is now rejected.** That
  is the WebAuthn spec's signal of a cloned authenticator or a replayed
  assertion; Ember previously recorded it and signed the user in anyway.
  Authenticators that don't implement a counter at all (most phones and
  laptops) are unaffected, as the spec requires.
- **Hardened the login endpoint against memory exhaustion.** Every attempt runs
  a 64 MiB argon2id derivation — including attempts for usernames that don't
  exist — and nothing bounded how many could run at once, so enough concurrent
  attempts could push the process into an out-of-memory kill. Derivations are
  now capped at four in flight; the rest queue.
- The login rate-limiter's internal table is now bounded, so traffic spread
  across a large address range can't grow it without limit.
- **Closed two SSRF bypasses in the private-address block.** `http://[::]/` and
  IPv4-compatible addresses like `http://[::127.0.0.1]/` slipped past the guard
  that stops Ember fetching internal hosts, because Go only normalizes the
  IPv4-*mapped* form (`::ffff:a.b.c.d`) when comparing against IPv4 ranges — so
  the loopback check never matched, while the connection itself reached
  localhost. The whole `::/96` range is now blocked (nothing routable lives
  there). Affected every outbound fetch: adding a feed, discovery, the poller,
  and the image proxy.
- **Web Push notifications had a data race that could corrupt payloads.** A
  single encoded notification was shared across the goroutines fanning out to
  each of your devices, and the push library writes into the spare capacity of
  the buffer it's handed — so concurrent sends could scribble over each other.
  Each send now gets its own copy.
- **Directory listings are no longer served for static asset paths.** Requesting
  `/assets/` returned an index of every built file, and because that path also
  carries a year-long `immutable` cache header, the listing was cached as if it
  were a content-hashed asset. Directory requests now return 404; single-page-app
  routes are unaffected.
- **Digest emails now use an unpredictable MIME boundary.** The boundary was
  guessable, and article titles are written into the plain-text part unescaped,
  so a crafted title could close the part early and forge additional MIME
  sections in the message. Boundary generation now fails the send outright
  rather than falling back to a fixed value.

### Changed

- **The interface stays responsive while feeds are being fetched.** Ember opens
  a single SQLite connection because the database allows only one writer, which
  meant every *read* — the article list, sidebar counts, search — had to queue
  behind whatever the background poller happened to be writing. Reads now go
  through a second, read-only connection pool that runs alongside the writer, so
  a refresh in progress no longer stalls the UI. Worst-case read latency during
  a feed fetch drops from ~166ms to ~23ms, roughly 1.45x faster on a typical
  sidebar load. Note for operators: this raises Ember's SQLite page-cache
  budget from 64 MiB to about 128 MiB (the read pool is deliberately capped at
  four connections of 16 MiB rather than inheriting the writer's 64 MiB each).
  If the read pool can't be opened for any reason, Ember logs a warning and
  serves reads from the write connection exactly as before.
- Bumped Go runtime dependencies: `github.com/mmcdole/gofeed` 1.3.0 → 1.4.0
  (which moves to `goxpp/v2` and drops five transitive dependencies),
  `github.com/pressly/goose/v3` 3.27.2 → 3.27.3, `golang.org/x/crypto`
  0.53.0 → 0.54.0, `golang.org/x/net` 0.56.0 → 0.57.0, and
  `modernc.org/sqlite` 1.53.0 → 1.55.0.
- Bumped SPA build/dev tooling: Svelte 5.56.4 → 5.56.8, Vite 8.1.3 → 8.2.0,
  `@sveltejs/vite-plugin-svelte` 7.1.4 → 7.2.0, svelte-check 4.7.1 → 4.7.4,
  `@testing-library/jest-dom` 6 → 7, `@types/node` 26.1.0 → 26.1.2, jsdom
  29 → 30.0.1, and `@playwright/test` 1.61.1 → 1.62.1. These are dev-only and are
  not bundled into the Ember binary. TypeScript is deliberately held at 6.x:
  svelte-check 4.7.4 supports TypeScript 7 only with both TypeScript 6 and 7
  installed side by side and an extra `--tsgo` flag, which is a build-tooling
  migration rather than a version bump.

### Fixed

- **Scheduled OPML exports written before noon UTC are no longer lost.** The
  export filename was built from a Go time layout that had `.opml` folded into
  it, and Go reads the `pm` in that extension as the AM/PM marker — so every
  export produced between 00:00 and 11:59 UTC was written as `.oaml` instead.
  Those files landed on disk but never matched the `.opml` filter behind the
  admin export list, leaving them invisible in **Settings → Database** and
  impossible to download or delete from the UI. Affects 0.9.4 and 0.9.5;
  existing `.oaml` files can be renamed to `.opml` to bring them back into the
  list.
- **Articles no longer stay hidden waiting for their AI summary.** When
  summaries are enabled, a new article was held back until the model had
  finished with it — with no time limit. On CPU-only inference that meant
  minutes of an apparently empty reader, and an article dropped from a full
  summary queue stayed invisible until Ember was restarted. Articles now appear
  once they've waited longer than a grace window (2 minutes by default), with
  the summary filling in when it's ready. Tune it in **Settings → Language
  model → Article visibility** or with `EMBER_SUMMARY_GRACE_SECONDS`; `0` shows
  articles as soon as they're fetched. (#162)
- **Articles dropped from a busy summary queue are picked back up.** The queue
  is bounded, and when it was full the article was silently skipped — it then
  had no summary, so the gate above hid it, and the only thing that retried was
  a sweep that ran once at startup. That sweep now runs every poll cycle.
- **Turning on the daily digest works again.** Saving from Settings → Digest
  always failed with a generic "invalid request body" error, so the feature
  could not be enabled at all from the UI. Ember was rejecting a request its
  own interface had built: the settings it sends back on save included two
  read-only fields (`user_id`, `last_sent_at`) that the save endpoint didn't
  accept. The endpoint now accepts and ignores them — so fetching your digest
  settings, changing one, and saving them back works from any client — and the
  interface no longer sends them in the first place. Which account the settings
  belong to is still taken from your session, never from the request, so the
  accepted field can't be used to change someone else's digest.
  Thanks to @Zaptorg for the report and the root-cause analysis (#161).
- **The All Unread badge could drift below the real count while you read.**
  Marking an article read from *Starred*, *Read Later*, *Shared*, or a board
  decremented the All Unread badge even when that article was older than the
  unread window — so the badge subtracted something it had never counted, and
  showed fewer unread articles than the All Unread list actually contained.
  Those four views are deliberately not windowed (they show saved items of any
  age), which is why only they were affected. The badge self-corrected on the
  next server refresh, but that refresh is debounced and restarts on every
  read, so while you were scrolling through a list it stayed wrong.
- **Newsletter mail addressed to several of your inboxes was being dropped.**
  The inbound SMTP server overwrote the recipient on each `RCPT TO` instead of
  collecting them, so when one message was addressed to multiple Ember inboxes
  only the last one received it. Nothing bounced — the sender saw a normal
  success — so the loss was silent.
- **Feeds that return unparseable content no longer get retried forever.** Only
  *fetch* failures widened the retry interval; a feed that fetched fine but
  failed to parse kept being re-requested at the floor interval indefinitely
  (~48 requests a day at the default, aimed at someone else's origin). Parse
  failures now back off on the same curve as fetch failures.
- **OPML import now reports how many subscriptions it actually added.** It
  previously counted every feed in the file, so re-importing the same OPML
  claimed to have added everything again when it had added nothing.
- **Backup and OPML-export retention only touches files Ember created.** The
  prune and delete paths matched any `.db` / `.opml` file in the configured
  directory. Since both directories are admin-settable, pointing one at a
  directory holding the live `ember.db` could have deleted the database in use.
  They now match only Ember's own `ember-<timestamp>.db` / `.opml` naming.

### Added

- **Turn AI summaries off for a single feed.** Feeds you skim don't need a
  summary, and on CPU-only inference they cost minutes of work each. The feed's
  **⋯** menu in the sidebar gains **Don't summarize** — the feed keeps updating
  normally, it just stops being sent to the model, and its existing summary
  cards disappear from your view. Switching it back on re-queues the articles
  that were skipped while it was off, so you don't have to wait for the feed to
  publish something new. The entry only appears while AI summaries are enabled
  on the server. It's a per-account choice: because a summary is stored once and
  shared by everyone subscribed to a feed, Ember only skips the inference when
  every subscriber has opted out — otherwise the work still happens and your
  copy simply arrives without the summary card. The "Summarizing N articles"
  indicator no longer counts work for feeds you've opted out of.

- **Optional device verification for passkey sign-in.** A new **Require device
  verification** toggle in Settings → Passkeys (or `EMBER_PASSKEY_REQUIRE_UV=1`)
  makes passkey sign-in demand a PIN, fingerprint, or face scan, so a passkey
  becomes two factors rather than possession alone. Off by default, because a
  passkey enrolled on a security key with no PIN configured would stop working
  and need re-registering — turn it on once you know every registered passkey
  can verify. Phones and laptops (Touch ID, Windows Hello) always verify, so
  they're unaffected either way.

## [0.9.5] - 2026-07-09

### Added

- **Clear every unread article in one click.** The **All Unread** row in the
  sidebar gains a hover **"Mark all as read"** button that marks your whole
  unread set read — not just the articles currently loaded on screen. (The
  article-list "Mark all read" pill still marks only what you've paged in, so
  both affordances remain available.)
- **Update notifications.** Ember now checks once a day whether a newer release
  is out and shows admins an **"update available"** hint in Settings → About
  plus a dismissible banner (dismissal is remembered per version). The check is
  a plain, unauthenticated read of the public GitHub releases API — no data
  about your instance is ever sent, and only admins see the hint. Turn it off
  with `EMBER_DISABLE_UPDATE_CHECK=1` or the **Check for updates** toggle in
  Settings.

### Changed

- **Staying logged in no longer kicks you out mid-session.** Sign-ins used to
  expire a fixed 24 hours after login even if you were actively reading. The
  24-hour window is now an *idle timeout* that slides forward every time you use
  Ember, so an active session keeps itself alive — up to 30 days from the
  original sign-in, after which you'll re-authenticate. Admins can still tune the
  idle window (env `EMBER_SESSION_TTL` or Settings → Sessions); the 30-day
  ceiling is fixed. Session cookies remain persistent — they survive a browser
  restart, while a private/incognito window still discards them when it closes.
- Bumped runtime dependencies: `github.com/go-chi/chi/v5` 5.3.0 → 5.3.1,
  `github.com/pressly/goose/v3` 3.27.1 → 3.27.2, and the Svelte SPA runtime
  5.56.3 → 5.56.4. Build/dev tooling was also updated (Vite 8.0.16 → 8.1.3,
  Vitest 4.1.9 → 4.1.10, `@sveltejs/vite-plugin-svelte` 7.1.2 → 7.1.4,
  svelte-check 4.6.0 → 4.7.1, `@types/node` 26.0.0 → 26.1.0, Playwright) — these
  are dev-only, not bundled into the Ember binary.

### Security

- Bumped the Go toolchain to 1.26.5 to pick up the standard-library fix for
  `GO-2026-5856` (an Encrypted Client Hello privacy leak in `crypto/tls`). Ember
  doesn't use ECH, but the bump keeps the bundled runtime current with the Go
  security release.

## [0.9.4] - 2026-06-29

### Added

- **Back up and restore your filters.** Settings → Filters → the filter editor
  now has **Export** (downloads your rules as a JSON file) and **Import** (loads
  them back, e.g. on another instance). Imported rules are validated like a
  manual add; anything invalid or beyond the per-user cap is skipped and
  reported.
- **The database backup directory is now configurable.** Set a custom absolute
  path in **Settings → Database → Backups → Directory** instead of the fixed
  `/data/backups`. Point it at a bind-mounted host path so backups live on a
  disk you control (the UI reminds you, and the docs walk through the compose
  bind-mount setup). Empty resets to the default; the scheduled job and the
  manual "Back up now" both honor it.
- **The OPML export gains the same controls as DB backups** — a configurable
  **Directory** (`opml_export_dir`, default `/data/exports`) and **Keep**
  retention (`opml_keep`) under Settings → Database → OPML export (set up the
  same way: bind-mount + chown), plus a manual **Export now** button and a list
  of recent exports.
- **Delete individual backups and OPML exports** from Settings → Database — each
  file in the list now has a Delete button (name-validated server-side, so it
  can't reach anything outside the configured directory).

### Fixed

- **Deleting a backup or OPML export now explains a permissions failure.** When
  the backup/export directory isn't writable by the server (the common case for
  a bind-mounted host path that hasn't been made owned by the container user,
  UID 65532), the Delete button now reports an actionable message instead of a
  generic "internal error" — matching the existing "Back up now" / "Export now"
  behavior.
- **Settings → Mobile clients**: the Fever URL and API-key boxes now line up —
  the key row's longer hint was squeezing its input narrower than the URL row's.
- **Settings → Database**: the "Clean up now" button now uses the same filled
  style as the other action buttons (it was an odd outline variant) and reuses
  the scheduled cleanup window instead of a separate, redundant days field.
- **The filter editor's buttons now match the rest of Settings.** The filter
  editor used an older button style; its buttons now use the standard Settings
  look with the same hover states, and **Export**/**Import** are the primary
  orange like **Add filter**. The Settings segmented toggles (e.g. Cards /
  Compact, On / Off) also gained a hover state.
- **Settings → Import & migrate**: importing an OPML file is now independent of
  the Tiny Tiny RSS section. It shows its own status in the OPML card — the
  button reads "Importing…" while it runs and reports the result right there —
  instead of surfacing under TT-RSS and disabling that form with no nearby
  feedback. The Tiny Tiny RSS card was also flattened — the live-migration form
  shows directly with **Start migration** and **Upload export file** side by
  side, replacing the segmented tabs whose inactive tab looked like a dead
  button.
- A story you'd already read could reappear as unread when a **duplicate**
  arrived later — a second feed publishing the same story, or the same feed
  re-publishing it under a new id. Cross-feed dedup previously only swept
  duplicates that existed at the moment you read; copies ingested afterward came
  in unread and the read original couldn't suppress them. Ingest now inherits the
  read state from an already-read cluster sibling, so a read story's late
  duplicates stay read instead of resurfacing in Fresh / All Unread.

### Security

- Defense-in-depth hardening from a full security audit (which found no
  exploitable issues): the login endpoint now returns an explicit allowlisted
  field set rather than the raw user record (so a future model field can't
  silently leak), search queries are length-capped before reaching SQLite, and
  filter-validation errors no longer surface the internal package prefix. Also:
  the CSRF cookie is now `SameSite=Strict` (matching the session cookie), the
  admin favicon URL is restricted to a same-origin path or `https://` (no
  `javascript:`/`data:`), and a "mark all read" scoped to an unknown board or
  category id returns 404 instead of a silent no-op.
- **The new per-file Delete (backups / OPML exports) is hardened against path
  traversal.** The server resolves the file by matching the requested name
  against the directory's own listing before removing it, so the request value
  never reaches the filesystem call and a crafted name can't escape the
  configured directory. Defense-in-depth — valid deletes are unaffected.

### Changed

- Bumped Go runtime dependencies `golang.org/x/crypto` 0.52.0 → 0.53.0,
  `golang.org/x/net` 0.55.0 → 0.56.0, and `modernc.org/sqlite` 1.52.0 → 1.53.0
  (plus transitive `x/sync`, `x/sys`, `x/text`, `modernc.org/libc`).

## [0.9.3] - 2026-06-22

### Added

- **Article images load through Ember instead of the publisher's CDN** — the
  card thumbnail and the reader's lead image are now served from Ember's own
  origin (`/api/img`) rather than fetched directly from a publisher CDN. Content
  blockers and tracker-blockers (uBlock Origin, Privacy Badger, …) match on
  those CDN domains and were silently stripping lead images (e.g. Fox News'
  `a57.foxnews.com`); routed same-origin they load normally. Source URLs are
  signed by the server, so the endpoint only fetches images Ember itself
  selected — it's not an open proxy.
- **Links in articles open in a new tab** — every link inside an article's body
  now opens in a separate browser tab (with `rel="noopener noreferrer"`), so
  following a link no longer navigates you out of Ember.

### Changed

- **"Mark all read" lets you finish the article you're reading** — marking
  everything read while an article is open now greys that card out but keeps it
  in the list (and in the reader pane) so you can keep reading. The next "Mark
  all read" hides it.
- **New articles no longer interrupt your scroll** — while you're browsing Fresh
  or All Unread, articles that arrive in the background are held back instead of
  being inserted into the list under your cursor. They load with the next batch
  of cards — for example when you hit "Mark all read" — so you never have to
  scroll back to the top to find them. "Refresh feeds now" still surfaces them
  immediately.

### Fixed

- **News articles whose image is delivered via Media RSS now show a picture** —
  many publishers (e.g. Fox News) attach the lead image as a `<media:content>`
  or `<media:thumbnail>` element rather than an enclosure or an inline `<img>`.
  The parser didn't read those, so those articles came through image-less. It
  now extracts the image from Media RSS. Applies to newly-fetched articles.
- **BleepingComputer articles no longer carry in-body ads** — BleepingComputer's
  feed ships only a short excerpt, so Ember extracts the full story from the
  page, which dragged in sponsored banners and an end-of-article promo block.
  Those are now stripped via a curated per-publisher rule; feeds we haven't
  vetted are left untouched. Applies to newly-fetched articles.
- **OPML import now keeps your folders** — feeds nested inside a folder were
  imported uncategorized: the folder (category) was created but the feeds landed
  outside it. They're now filed under their folder's category, so an imported
  subscription list comes in organized the way it was exported. Nested
  sub-folders flatten into their top-level folder (Ember categories are flat).

### Security

- **Bumped `undici` 7.26.0 → 7.28.0** (transitive devDep via `jsdom`) to patch
  [GHSA-vmh5-mc38-953g](https://github.com/advisories/GHSA-vmh5-mc38-953g)
  (TLS certificate validation bypass via dropped `requestTls` in SOCKS5
  `ProxyAgent`, high) and
  [GHSA-pr7r-676h-xcf6](https://github.com/advisories/GHSA-pr7r-676h-xcf6)
  (cross-user information disclosure via shared cache whitespace bypass,
  medium). Dev-only — `undici` is not bundled into the Ember binary.

## [0.9.2] - 2026-06-15

### Fixed

- **Opening a syndicated story no longer leaves a duplicate in the list** — when
  a story runs in two feeds you follow, the list shows one copy. Opening it
  marked only that copy read, so a few seconds later the background refresh
  surfaced the other feed's copy as a "new" unread duplicate of the article you
  were already reading. Reading a story now marks its cross-feed copies read as a
  unit (the same way "Mark all read" already does), so the duplicate no longer
  pops up. Starring, saving for later, and tagging still apply to the single copy
  you chose.

## [0.9.1] - 2026-06-15

### Added

- **Lead image in the reader** — when a feed provides an article image (the same
  one shown on the list card) but the article body has no inline image, the
  reader now shows it as a lead image at the top, so the story no longer looks
  image-less.

### Changed

- **"Mark all read" clears Fresh and All Unread as you go.** In the unread-only
  views (Fresh, All Unread), marking read now drops the read cards and pages in
  the next unread batch, so the column reflects what's left to read. Today,
  Starred, Read Later, and Shared keep their cards, since those views show read
  and unread together. Duplicated stories are cleared as a unit — marking the
  shown copy read also marks its hidden cross-feed copies read, so a duplicate
  doesn't pop back as unread.

### Fixed

- **Unread badges no longer collapse to zero when a story has a read or
  out-of-window duplicate** — cross-feed dedup was suppressing every visible
  unread copy of a story whenever any lower-id duplicate existed anywhere in the
  database, even if that duplicate was already read or outside the reading
  window. Fresh, All Unread, and per-category badges could drop to 0 while the
  per-feed badges still showed counts. Dedup now matches duplicates against the
  same unread/window/summary filter as the view, so a row is only hidden behind
  a copy you would actually see. Per-feed badges and columns now dedup too, so
  each duplicated story is counted and shown once and the per-feed badges sum to
  the All Unread total.
- **Article titles no longer show raw HTML entities** — feeds that encode their
  titles (e.g. Atom `type="html"` with `&#8217;` curly quotes, or entity-escaped
  ampersands) were stored verbatim and rendered as plain text, so titles like
  "Roblox exec says it is &#8216;not enough anymore&#8217;" leaked the entity
  codes. Titles are now decoded to display text on ingest, matching how article
  bodies are already handled. Affects newly fetched articles.
- **Unread/fresh badges stay consistent with the lists they label** — several
  sidebar and header counts fell back to a non-deduped, non-windowed value when
  the server's authoritative deduped + windowed count was legitimately 0 (or a
  zero-count folder was omitted from the per-category map), so a badge could
  disagree with the cards it summarizes. Badges now honor a genuine server 0,
  treat a missing folder as 0 (not "unknown"), count the rendered list rather
  than the raw loaded page, and reconcile against the server after optimistic
  read toggles.
- **Starred / Read Later badges match their lists** — these two counts ignored
  the muted-feed exclusion and cross-feed deduplication their lists apply, so a
  badge could exceed the cards shown when a starred/saved item lived in a muted
  feed or was duplicated across feeds; they now share the same filters.
- **Sidebar counts no longer lag after "Mark all read"** — a slower, in-flight
  count request could overwrite the up-to-date numbers with stale ones, leaving
  e.g. "All Unread 53" hanging over an empty column until the next poll. The
  newest count now always wins.
- **New articles appear right after Refresh** — clicking Refresh briefly polls
  for the feeds it just kicked off, so freshly-pulled articles surface without
  reselecting the view or reloading the page.
- **Reading position kept on mobile** — returning from an article to the list no
  longer jumps back to the top of the column.
- **Fever sync completeness** — `unread_item_ids` / `saved_item_ids` now return
  the complete set (they were capped at 200) and are no longer cross-feed
  deduplicated, so a Fever client's unread tally matches what Ember actually
  holds. The `items` call honors `since_id` / `max_id` / `with_ids` paging and
  reports the true `total_items`, letting clients sync the full backlog instead
  of only the latest 50.

## [0.9.0] - 2026-06-10

### Added

- **Create folders** — a **+** in the sidebar Folders header makes a new folder
  and drops straight into renaming it.
- **Collapse / expand all folders** — a one-click toggle in the Folders header;
  the collapsed state is remembered across reloads.
- **Drag feeds into any folder** — every folder header (including empty ones and
  Uncategorized) is now a drop target, so a feed can be moved into a folder that
  has no rows to drop onto.
- **Keyboard search preview** — the type-ahead dropdown is arrow-key navigable
  (↑/↓ to highlight, Enter to open) with a **Load more** row that fetches the
  next 6 previews.
- **Edit feed** — the sidebar feed menu now has an _Edit_ option to change a
  feed's title, folder, or **source URL**. Changing the URL re-points the
  subscription to the new feed (validated and re-fetched) without affecting
  other subscribers.
- **Load more paging** — article columns load 50 at a time (search results 25)
  behind a _Load more_ button instead of a fixed cap.
- **Reading & search windows** — admin settings under **Settings → Feeds** bound
  how far back the reading views (default 24h) and full-text search (default
  48h) reach, both capped at a fixed rolling 1-week retention window.
- **Automatic retention** — articles past the 1-week window are pruned daily;
  starred, read-later, board-pinned, and shared articles are kept indefinitely.

### Changed

- **"Mark all read" now marks only the loaded articles.** With Load more paging,
  the article-column _Mark all read_ marks the cards currently shown, not the
  entire view — anything behind _Load more_ stays unread. The sidebar's per-feed
  _Mark feed read_ still marks the whole feed.
- **Unread counts and windows are unified.** Sidebar badges (All Unread,
  per-folder, per-feed) use the same window, summary gate, and cross-feed dedup
  as the list they summarize, so a badge always matches its column. The unread
  window extends back to your previous login (floored at the reading window,
  capped at retention).
- **New feeds pull only the last 24 hours** on first fetch (was 48h); existing
  feeds add only genuinely new items.

### Fixed

- **"Refresh feeds now"** now triggers an actual fetch of every subscribed feed
  to pull new articles, instead of only re-reading already-stored ones.
- Renaming a folder (and editing a feed's title) **pre-selects** the existing
  text so you can type the new name without clearing it first.
- The empty reading pane is centered and no longer shows the redundant "Pick a
  story" heading.
- Settings links use the brand link color instead of default browser blue, and
  the email-inbox setup-docs link now points at the live docs page (was a dead
  `/docs/...` path).

### Security

- **Changing your email now requires your current password**, and email
  addresses must be unique — a borrowed session can't quietly redirect your
  digest mail, and two accounts can't share an address.
- **Hardening pass.** The outbound-fetch SSRF guard now also refuses non-web
  service ports (SSH, databases, Redis, …); the readability extractor and
  decoded inbound-email parts are size-capped to prevent memory exhaustion;
  editing a feed's source URL is rate-limited like adding one; OPML/TT-RSS
  import errors no longer echo internal detail; and search paging is bounded.
  See [docs/SECURITY_FINDINGS.md](docs/SECURITY_FINDINGS.md) (Review #3).

## [0.8.7] - 2026-06-08

TT-RSS full migration (subscriptions, folders, starred/archived) and fail-fast
admin bootstrap. See the
[v0.8.7 release](https://github.com/brandonhon/ember/releases/tag/v0.8.7).

[Unreleased]: https://github.com/brandonhon/ember/compare/v0.9.6...develop
[0.9.6]: https://github.com/brandonhon/ember/compare/v0.9.5...v0.9.6
[0.9.5]: https://github.com/brandonhon/ember/compare/v0.9.4...v0.9.5
[0.9.4]: https://github.com/brandonhon/ember/compare/v0.9.3...v0.9.4
[0.9.3]: https://github.com/brandonhon/ember/compare/v0.9.2...v0.9.3
[0.9.2]: https://github.com/brandonhon/ember/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/brandonhon/ember/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/brandonhon/ember/compare/v0.8.9...v0.9.0
[0.8.7]: https://github.com/brandonhon/ember/releases/tag/v0.8.7
