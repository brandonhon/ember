# Hero image candidates

Dark-mode candidates for the docs site hero (currently the logo on a circle
gradient in `docs/index.md`). Pick one; the winner gets moved into
`docs/public/` and wired into the `hero.image.dark` (and a light variant
captured) — or used as a full-bleed screenshot below the hero.

All captured at 1440×900 @2× (2880×1800) from the test-mode binary's seeded
fixtures (`cmd/ember/seed.go`), dark theme.

| File | Shot |
|------|------|
| `hero-1-login-dark.png` | Login page — brand panel + sign-in card |
| `hero-2-threepane-summary-dark.png` | Three-pane reader with the AI summary card (feature showcase) |
| `hero-3-threepane-reader-dark.png` | Three-pane reader, clean long-form article (no summary card) |

Regenerate: the seed feeds the screenshots, so a throwaway `web/e2e/_hero.spec.ts`
(deleted after each run) drives the capture against `make embed build` + the
test-mode binary on `:8090`. See `web/scripts/screenshots.mjs` for the
docs-screenshot pipeline these mirror.
