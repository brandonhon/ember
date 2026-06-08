---
layout: home
title: Ember
titleTemplate: Self-hosted RSS reader with on-device AI summaries

hero:
  name: Ember
  text: A reader for people who read.
  tagline: Self-hosted RSS aggregation with an optional on-device LLM and a paper-and-ink interface. One Go binary, one container, one tab.
  image:
    # Theme-inverted on purpose: a dark UI screenshot reads strongest against
    # the cream paper background; the light screenshot pops against the
    # warm-dark page in dark mode. Both are real screenshots of the running
    # app, captured at 2880×1800 retina.
    light: /screenshots/hero-2-threepane-summary-dark.png
    dark: /screenshots/hero-2-threepane-summary-light.png
    alt: Ember three-pane reader with AI summary card
  actions:
    - theme: brand
      text: Get started
      link: /getting-started
    - theme: alt
      text: View on GitHub
      link: https://github.com/brandonhon/ember

features:
  - icon: 📰
    title: Three-pane reader
    details: Sidebar of feeds and folders, the article list, and a focused reader. Keyboard navigation (j/k/r/m/s/?), drag-to-reorder.
  - icon: 🧠
    title: Local AI summaries
    details: Optional Ollama integration produces a paragraph + bullet summary for each article. Pull, swap, and tune models from the admin UI. Strips newsletter / podcast promos from the body.
  - icon: 🔎
    title: FTS5 full-text search
    details: SQLite's FTS5 powers a dedicated search view + saved searches surfaced in the sidebar. Per-article user tags filter the list down further.
  - icon: 🪶
    title: Single binary
    details: Pure-Go SQLite, embedded Svelte SPA, no CGO. A single ~25 MB binary that runs anywhere and behind any reverse proxy.
  - icon: 🎨
    title: 8 themes + custom palette
    details: Auto (matches OS), Light, Dark, Solarized, Sepia, Nord, Gruvbox, High contrast, plus a custom theme that derives the rest of the palette from 3 colors you pick.
  - icon: 🔐
    title: Hardened by default
    details: argon2id passwords, SameSite=Strict cookies, CSRF double-submit, SSRF block on outbound fetches, generic error responses, govulncheck-clean stdlib.
  - icon: ⚖️
    title: Smart cross-feed dedup
    details: Tracking-param-stripped canonical URL + title-fingerprint clustering (48h window) collapse syndicated wire stories into one row. Click the "Also in N feeds" pill to expand the sibling list with per-feed read/star state.
  - icon: 🎯
    title: Rules engine
    details: Five actions (mark_read, star, hide, tag, add_to_board), eight match fields including feed, tags, published_at, has_image. Per-rule priority and a Preview button that counts last-7-day matches before you save.
---

And plenty more under the hood: **migrate your library** (OPML subscriptions, or a full Tiny Tiny RSS migration — subscriptions, folders, and starred/archived articles), a **Fever-compatible API** (Reeder, FeedMe & co. via a random per-user token), **passkey sign-in** (Touch ID / Face ID / hardware keys), an opt-in **daily digest email**, **subscribe-by-URL** discovery (including YouTube channels and Mastodon profiles), **15-second auto-refresh** with a favicon unread dot, and **live admin controls** for hot-swapping the LLM model, tuning generation params, and scheduling backups / cleanup / OPML exports.

## Why?

Most RSS readers are either bloated cloud services that mine your reading habits, or unmaintained scripts from the Google Reader exodus. Ember is what an opinionated 2026 reader looks like: a single Go binary you run on your own box, a paper-and-ink interface, and — only if you want it — small-local-LLM summaries for the days you can't read 300 articles.

## Quick install

```sh
git clone https://github.com/brandonhon/ember.git
cd ember/deploy
cp .env.example .env
# Set EMBER_SESSION_KEY and EMBER_ADMIN_PASSWORD
docker compose up -d
```

Open `https://localhost`, log in, click a starter pack. You'll see articles within a minute.

See [Getting started](/getting-started) for details.
