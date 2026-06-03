# Web Push notifications

::: info Coming in a future release
Browser push notifications (Web Push / VAPID) are **in active development** and are not part of the current `v0.7.x-beta` stream. This page is a placeholder while the implementation stabilizes.
:::

## What's planned

- Opt-in per browser / installed PWA from **Settings → Notifications**.
- VAPID keypair auto-generated and persisted server-side — no third-party push gateway.
- Test-send button to verify the round trip.
- A `notify` action in the rules engine that fires a push when a matching article arrives.

## Requirements once it ships

- A **trusted TLS certificate**. Service workers refuse to register over self-signed / `tls internal` certificates, so push, offline cache, and PWA install all break together on a homelab deployment without a real cert. The doc that lands with the feature will walk through the DNS-01 / Let's Encrypt path for LAN-only deployments.

## Tracking

Implementation lives on the `develop` branch. Follow the milestone or open issues at <https://github.com/brandonhon/ember/issues> for progress.
