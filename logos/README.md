# Ember — Logo & Favicon Assets

Five logo concepts to choose from. Open **ember-logo-concepts.html** in a browser to
compare them on light/dark, as wordmark lockups, and at favicon sizes.

## Concepts
| # | Name | Best for |
|---|------|----------|
| 01 | Ignite (spark) | Modern, AI-summary tie-in. Crisp at any size. |
| 02 | Kite | kite-public lineage. (Softens most at 16px.) |
| 03 | Feedmark | RSS + feed-list + "E". **Recommended favicon.** |
| 04 | Flame | Literal "ember". Warmest. Great splash mark. |
| 05 | Coal | App-icon tile. Native look on home screens. |

## Files per concept (replace NN with 01–05)
- `ember-NN-*.svg`            — source vector (scales infinitely)
- `png/NN-*-{16,32,48,180,512}.png` — raster renders
- `favicons/favicon-NN-*.ico` — multi-size .ico (16/32/48)
- `png/NN-*-apple-180.png`     — apple-touch-icon
- `png/NN-*-maskable-512.png`  — PWA maskable icon (safe padding)

> Concept 04 (Flame) ships a `-small` solid variant used for the <=48px favicon sizes
> so the inner cut doesn't close up; the full mark with negative space is used at 180/512.

## Wiring it into the app (pick your concept, then in index.html <head>)
```html
<link rel="icon" type="image/svg+xml" href="/ember-03-feedmark.svg">
<link rel="icon" type="image/x-icon" href="/favicon.ico">
<link rel="apple-touch-icon" href="/apple-touch-icon.png">
<link rel="manifest" href="/manifest.webmanifest">
<meta name="theme-color" content="#c2451d">
```

manifest.webmanifest:
```json
{
  "name": "Ember", "short_name": "Ember",
  "theme_color": "#c2451d", "background_color": "#f6f2e9",
  "display": "standalone", "start_url": "/",
  "icons": [
    { "src": "/icon-512.png", "sizes": "512x512", "type": "image/png" },
    { "src": "/icon-maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable" }
  ]
}
```

In the Go build, drop the chosen favicon/SVG/PNGs into `web/public/` so Vite copies
them to `dist/`, where `embed.FS` will bundle them into the single binary.

Palette: ember #c2451d · ember-soft #e8643a · gold #e0992b · ink #211d18 · paper #f6f2e9
