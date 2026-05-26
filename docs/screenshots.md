# Screenshots

<!-- Screenshots go here once captured. For now this page is a placeholder
     describing the major UI surfaces; once we have a working CI that builds
     the docs, the screenshot capture flow can write to docs/public/ and
     embed here. -->

## Three-pane reader

The default layout: sidebar of folders + feeds, the article list, and the reader. Drag-to-reorder folders and feeds within them. Right-click any feed for mute, mark-all-read, resummarize, or delete.

## Summary card

The AI summary appears between the title and the article body. Paragraph lead + 3 factual bullets, with the source thumbnail inline. Collapsible per article; the preference persists per user.

## Settings

Eight sections:

- **Profile** — change your password.
- **Preferences** — theme picker (8 presets + custom 3-color editor), density (cards / compact), AI summary on/off, article images on/off, scroll-to-mark-read on/off.
- **Reading stats** — today / week / 30-day read counts, top feeds.
- **Mobile clients** — Fever URL + your random API token.
- **Filters** — manage hide/star/mark-read rules.
- **Starter packs** — one-click curated feed bundles (Technology / Programming / Security / DevOps / World News).
- **Language model** (admin) — host probe, current model, installed list with switch + delete, pull form, tuning sliders.
- **Branding** (admin) — name, page title, favicon URL.
- **Database** (admin) — size, manual + scheduled backups, cleanup, OPML export.
- **Users** (admin) — create, edit, delete user accounts.

## Mobile

≤900px viewport: sidebar collapses into an off-canvas drawer. Article list and reader take turns at full width — selecting an article switches to the reader, the back button returns to the list. Below 520px, brand text hides so the search bar has room.

## Themes

- **Auto** — follows the OS `prefers-color-scheme`.
- **Light** / **Dark** — paper-and-ink defaults.
- **Solarized** — Ethan Schoonover's classic palette (light).
- **Sepia** — warm browns, e-reader friendly.
- **Nord** — cool blue-gray dark.
- **Gruvbox** — warm-tinted dark by morhetz.
- **High contrast** — black/white/yellow for low-vision users.
- **Custom** — pick 3 colors (paper, ink, accent), the rest derives via CSS `color-mix()`.
