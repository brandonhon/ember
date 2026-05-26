# Screenshots

Captured against the live docker stack via `make docs-screenshots` (a Playwright script under `web/scripts/screenshots.mjs`). All shots are auto-regeneratable; the build doesn't depend on them being current — re-run the script anytime the UI shifts.

## Three-pane reader

The default layout: sidebar of folders + feeds, the article list, and the reader. Scroll-to-mark-read, keyboard navigation (`j` / `k` / `r` / `m` / `s` / `?`), drag-to-reorder folders and feeds within them.

![Reader, desktop](/screenshots/reader-desktop.png)

## Article view

Summary card sits between the title and the body — paragraph lead + factual bullets with an inline thumbnail. AI ad-stripping removes newsletter signups, podcast promos, and "Comments" trailers from the body before display.

![Article, desktop](/screenshots/article-desktop.png)

## Settings — preferences

Theme picker (8 presets + custom palette), density toggle, scroll-to-mark-read on/off, AI summary on/off, article images on/off.

![Settings, preferences](/screenshots/settings-preferences-desktop.png)

## Settings — language model (admin)

Host probe (RAM/CPU/GPU) with a model recommendation. Installed-model table with per-row switch + delete. Pull form for new models. Sliders for temperature, top_p, num_ctx.

![Settings, LLM](/screenshots/settings-llm-desktop.png)

## Login

Paper-and-ink split layout. Branding (app name, page title, favicon) is admin-configurable from Settings → Branding.

![Login, desktop](/screenshots/login-desktop.png)

## Mobile

≤900px viewport: sidebar collapses into an off-canvas drawer. Article list and reader take turns at full width — selecting an article switches to the reader; a back arrow returns to the list. Below 520px, the brand text hides so the search bar has room.

<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; margin: 24px 0;">

![Reader, mobile](/screenshots/reader-mobile.png)

![Article, mobile](/screenshots/article-mobile.png)

</div>

## Themes

Eight presets cover light, dark, and accessibility needs. The "Custom" theme lets you pick 3 colors (paper, ink, accent) and the rest of the palette derives via CSS `color-mix()`.

- **Auto** — follows the OS `prefers-color-scheme`.
- **Light** / **Dark** — paper-and-ink defaults.
- **Solarized** — Ethan Schoonover's classic palette.
- **Sepia** — warm browns, e-reader friendly.
- **Nord** — cool blue-gray dark.
- **Gruvbox** — warm-tinted dark by morhetz.
- **High contrast** — black / white / yellow for low-vision users.
- **Custom** — your three colors.

## Regenerating these

Bring up the docker stack, subscribe to at least one starter pack, then:

```sh
make docs-screenshots
```

The script logs in, takes a tour through each surface at both desktop (1440×900 @ 2x) and mobile (390×844 @ 3x) viewports, and writes PNGs into `docs/public/screenshots/`. Commit the result — it's the same as any docs change.
