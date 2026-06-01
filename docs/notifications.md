# Notifications

Ember can push notifications to your browser or installed PWA — even when no
tab is open. Notifications use the standard Web Push (VAPID) protocol, so
there's no third-party push gateway in the loop: your browser talks to the
push service it's already paired with (Mozilla, Google, Apple), and your
Ember server is the only thing that can send to your devices.

## Operator setup

There is **no admin config** for push. On first start Ember generates a
VAPID keypair via `crypto/rand` and persists it to `app_settings`. The
public key is exposed to the SPA via `GET /api/me/push-vapid-public-key`;
the private key never leaves the server.

One requirement, though, is the size of a billboard:

> Service workers — the browser-side machinery push notifications run
> through — require a **trusted TLS certificate**. Self-signed / Caddy
> `tls internal` certs are not enough. The browser refuses to load
> `/sw.js` over an untrusted cert, with no override path, which means
> push, offline cache, and PWA install **all break together**.

The error in the browser console looks like:

```
Failed to register a ServiceWorker for scope ('https://your-host/') with
script ('https://your-host/sw.js'): An SSL certificate error occurred when
fetching the script.
```

If you see this, you need a real cert.

### Getting a real cert for a LAN deployment

The bundled `deploy/Caddyfile` gets a Let's Encrypt cert automatically when
**all three** of these are true:

1. `EMBER_HOSTNAME` is a real DNS name pointing at the host.
2. `CADDY_EMAIL` is a real email address.
3. Ports 80 / 443 are reachable from the internet (HTTP-01 / TLS-ALPN-01
   challenge).

For a LAN-only deployment that doesn't accept inbound public traffic, use
the **DNS-01 challenge**. You'll need:

- A real domain you control (e.g. `example.com`).
- An API token at your DNS provider (Cloudflare, Route53, DigitalOcean, …).
- A custom Caddy build with that provider's plugin compiled in. The Caddy
  docs at <https://caddyserver.com/docs/automatic-https#dns-challenge>
  list the recognized providers.

Then your Caddyfile becomes something like:

```caddy
ember.lan.example.com {
  tls {
    dns cloudflare {env.CF_API_TOKEN}
  }
  reverse_proxy ember:8080
}
```

Caddy will request a cert via DNS-01 (which doesn't care whether anyone can
reach your server, only that you control the DNS), and every device on
your LAN trusts it because it chains to Let's Encrypt's public roots.

### Why not just trust Caddy's local CA on each device?

You can:

```sh
docker compose -f deploy/docker-compose.yml exec caddy \
  cat /data/caddy/pki/authorities/local/root.crt
```

…then install the resulting certificate as a trusted root on every device
you want to use Ember from. Works fine on desktops. **Doesn't work well on
phones**: iOS requires installing the cert as a configuration profile and
explicitly trusting it under Settings → General → About → Certificate
Trust Settings; Android requires Settings → Security → Encryption &
credentials → Install a certificate → CA certificate (which also
deliberately yells at you about MITM).

If push only needs to work on your laptops, the trust-the-local-CA path
is acceptable. For phones, prefer DNS-01.

## User flow

Once the cert situation is sorted:

1. **Settings → Notifications**.
2. **Enable on this device** → the browser asks for notification
   permission.
3. Approve → the SPA POSTs your subscription to the server.
4. **Send test notification** to confirm the round trip works.

Repeat on every browser / PWA install you want to receive pushes on. The
list under "Registered devices" shows the user-agent string and creation
time of each subscription. **Revoke** removes the device server-side; the
browser keeps a local subscription that the next push 410's on, which
triggers a second server-side cleanup. Either way the row goes away.

## Why notifications still feel sparse

Today push is **infrastructure only**: the buttons + endpoints exist but
nothing yet fires a notification on its own. The next feature on the
roadmap is a `notify` filter action — once that lands, a rule like
"`title contains 'breaking'` → notify" will push as soon as the matching
article arrives. Until then push is mostly useful for verifying setup or
for future hooks.

## What gets sent

The payload that arrives at your service worker looks like:

```json
{
  "title": "Ember test",
  "body":  "Notifications are working.",
  "url":   "/articles/123",
  "article_id": 123
}
```

`url` controls what opens on notification click; `article_id` is used to
collapse repeated notifications about the same article into one stack
entry (the `tag` attribute on `showNotification`).

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| "An SSL certificate error occurred when fetching the script" | Untrusted TLS cert. See above. |
| "Failed to register a ServiceWorker… insecure connection" | Plain HTTP. Service workers require HTTPS (or `localhost`). |
| Enable button does nothing, no permission prompt | Browser permission was previously denied. Reset under the site's lock icon → Permissions → Notifications. |
| Permission granted but test send returns `sent: 0` | The subscription POST failed (check Network tab) or the row was already cleaned up. Disable + re-enable. |
| Send test returns `sent: 1, removed: 0` but no notification appears | The OS is suppressing notifications (macOS Focus, iOS Focus / DND, Windows Quiet Hours). Check OS-level notification settings for your browser. |
