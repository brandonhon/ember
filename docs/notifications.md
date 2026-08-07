# Web Push notifications

::: warning Enrollment ships; automatic notifications don't yet
You can enrol a browser, manage the enrolled devices and send yourself a test
notification. **Nothing sends a push automatically** — no new-article trigger
exists, so once enrolled you will only ever receive the test message. Enrol if
you want to verify the plumbing on your deployment; don't expect to be told
about new articles by it yet.

For actually noticing new articles today, see
[what does tell you](#what-tells-you-about-new-articles-today).
:::

## What works today

From **Settings → Notifications**:

| | |
| --- | --- |
| **Enable** | Enrols this browser (or installed PWA). Asks for the browser's notification permission, subscribes with the server's VAPID key, and registers the subscription against your account. |
| **Turn off** | Appears in place of *Enable* once this browser is enrolled. Unsubscribes this browser and removes its entry in one step. |
| **Send test** | Fires a sample notification to every device registered to your account. This is the only thing that sends a push. |
| **Registered devices** | Every browser enrolled by your account, by user-agent. **Revoke** removes one; revoking the device you're on also unsubscribes it locally. |

Enrolment is **per browser**, not per account — each browser or installed PWA
you want notified has to be enabled from that browser.

## What doesn't work yet

- **No automatic notifications.** Nothing in the poller, ingest path or filters
  triggers a push. The delivery machinery is complete and tested — the trigger
  simply isn't written.
- **No `notify` filter action.** The rules engine can tag, star, mark read and
  mute; it cannot send a notification.

## What tells you about new articles today

- A **favicon dot** and a `(N) Ember` page-title prefix while the tab is open.
- An **OS-level numeric badge** on the app icon when installed as a PWA.
- The optional **[daily digest email](/configuration#optional-env-vars)**.

## Requirements

- **A trusted TLS certificate.** Service workers refuse to register over
  self-signed certificates, so Web Push, the offline cache and PWA install all
  fail together on a homelab deployment using Caddy's `tls internal`. Use a real
  certificate — the DNS-01 route works for LAN-only hosts that can't answer an
  HTTP-01 challenge. See [Hardening Caddy](/caddy-hardening).
- Nothing to configure to switch the feature on: the VAPID keypair is generated
  on first start and persisted server-side. If key generation fails, the push
  endpoints return `503` and the rest of Ember is unaffected.

## Operator notes

- **Deliverability contact.** Push services want a contact address for the
  sender, taken from `EMBER_SMTP_FROM`. When that isn't set Ember falls back to
  `mailto:admin@localhost` and logs a warning at startup — harmless for a
  private instance, but push services may deprioritise an unidentified sender.
- **No third-party push provider.** Ember signs and encrypts each message with
  its own VAPID keypair; there's no OneSignal-style intermediary holding your
  data. Delivery still goes through the browser vendor's push service (Google,
  Mozilla, Apple), as the Web Push standard requires — those services relay an
  encrypted payload they cannot read.
- **Dead subscriptions clean themselves up.** A push service replying `410 Gone`
  causes Ember to drop that subscription; the test-send response reports how
  many were sent and how many were removed.
