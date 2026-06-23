# Changelog

All notable user-facing changes to Ember are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Ember adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Per-tag [GitHub Releases](https://github.com/brandonhon/ember/releases) hold the
full commit-level list; this file curates the highlights and behavior changes.

## [Unreleased]

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

[Unreleased]: https://github.com/brandonhon/ember/compare/v0.9.3...develop
[0.9.3]: https://github.com/brandonhon/ember/compare/v0.9.2...v0.9.3
[0.9.2]: https://github.com/brandonhon/ember/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/brandonhon/ember/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/brandonhon/ember/compare/v0.8.9...v0.9.0
[0.8.7]: https://github.com/brandonhon/ember/releases/tag/v0.8.7
