# Changelog

All notable user-facing changes to Ember are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Ember adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Per-tag [GitHub Releases](https://github.com/brandonhon/ember/releases) hold the
full commit-level list; this file curates the highlights and behavior changes.

## [Unreleased]

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
- **Admin-configurable feed check interval** — the poll interval moved into its
  own Feeds settings section (default 30 minutes).

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

## [0.8.7] - 2026-06-08

TT-RSS full migration (subscriptions, folders, starred/archived) and fail-fast
admin bootstrap. See the
[v0.8.7 release](https://github.com/brandonhon/ember/releases/tag/v0.8.7).

[Unreleased]: https://github.com/brandonhon/ember/compare/v0.8.7...develop
[0.8.7]: https://github.com/brandonhon/ember/releases/tag/v0.8.7
