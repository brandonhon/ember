# Email newsletter inbox

::: info Coming in a future release
The per-user email-newsletter inbox is **in active development** and is not part of the current `v0.7.x-beta` stream. This page is a placeholder while the implementation stabilizes.
:::

## What's planned

Each user will get a unique address (e.g. `<random-handle>@<EMBER_EMAIL_DOMAIN>`). Mail sent there lands as articles in a synthetic per-user "Newsletters" feed — Substack, Beehiiv, mailchimp lists go through the same reader, filters, and digest as RSS items.

## Operator preview

When it ships, the operator will set `EMBER_EMAIL_DOMAIN` and arrange inbound mail delivery to Ember's SMTP listener (default `:2525`, frontable with Caddy `layer4` / postfix / haproxy). MX records are the operator's responsibility.

Security posture:
- Open-relay protection (mail to unknown handles is rejected with `550 5.1.1 no such mailbox`).
- No AUTH required or accepted (inbound-only MX).
- Per-message size cap configurable via `EMBER_EMAIL_MAX_BYTES` (default 25 MiB).

## Tracking

Implementation lives on the `develop` branch. Follow the milestone or open issues at <https://github.com/brandonhon/ember/issues> for progress.
