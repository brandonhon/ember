# Email newsletter inbox

Each Ember user gets a unique address (e.g. `01234ABCDEFG@mail.example.com`).
Mail sent there lands in a per-user "Newsletters" feed, alongside RSS articles.
Substack, Beehiiv, mailchimp lists — anything you'd otherwise read in email — go
through the same reader UI, the same filters, the same digest.

The feature is **off by default**. Setting `EMBER_EMAIL_DOMAIN` turns it on.

## What the operator has to do

You're terminating inbound SMTP for a domain. That requires:

1. **A domain you control with an MX record pointing at the Ember host.** A
   subdomain like `mail.example.com` is fine — keep your main MX for human
   mail.
2. **Inbound port 25 reachable**, OR a fronting relay (Caddy layer4, haproxy,
   nginx stream, postfix) forwarding to Ember's listener port.
3. **Set `EMBER_EMAIL_DOMAIN`** to the domain in the address (e.g.
   `mail.example.com`). Without it, the SMTP listener doesn't start and the
   inbox endpoints return `enabled: false`.

### Recommended setup: Caddy layer4 → Ember

Ember's listener binds `:2525` by default (privileged port 25 requires root
or `CAP_NET_BIND_SERVICE`). Front it with Caddy so port 25 stays reverse-
proxied:

```caddy
{
  layer4 {
    :25 {
      route {
        proxy ember:2525
      }
    }
  }
}
```

If you prefer postfix / native :25 binding, change `EMBER_EMAIL_LISTEN_ADDR`
to `:25` and run Ember with the capability.

## Env vars

| Variable | Default | Notes |
|---|---|---|
| `EMBER_EMAIL_DOMAIN` | (unset) | Required to enable. The host part of generated addresses. |
| `EMBER_EMAIL_LISTEN_ADDR` | `:2525` | SMTP bind address. |
| `EMBER_EMAIL_MAX_BYTES` | `26214400` (25 MiB) | Per-message size cap. |

## Security

- **Open-relay protection.** The listener accepts mail only when the envelope
  `RCPT TO` exactly matches an active `<handle>@<EMBER_EMAIL_DOMAIN>`. Every
  other recipient is rejected with `550 5.1.1 no such mailbox`.
- **No AUTH required, no AUTH accepted.** This is an inbound-only MX, not a
  submission service.
- **No SPF / DKIM verification today.** The handle itself is the auth token
  (~60 bits of entropy); spoofing requires guessing it. Verifying SPF / DKIM
  is a planned follow-up — for now operators worried about spoofed senders
  should front Ember with a real MTA (postfix + opendkim + spamassassin)
  that does the verification and forwards the clean messages over LMTP.
- **Body size capped** at `EMBER_EMAIL_MAX_BYTES`. Attachments are read but
  not extracted.

## Address rotation

If a handle gets sold or leaks, hit the **Rotate address** button in Settings
→ Email inbox. A new handle takes effect immediately; the old one keeps
accepting mail for **7 days** so existing newsletter subscriptions can be
updated without losing in-flight issues.

## What gets stored

Each incoming message becomes one article row. The article uses:

- **Title:** the `Subject:` header (or "(no subject)").
- **Author:** the `From:` display name, falling back to the address.
- **Body:** `text/html` part if present; falls back to `text/plain`. HTML is
  also converted to plain text for full-text search.
- **Published at:** the `Date:` header.

The synthetic feed has `kind='email'` so the RSS poller skips it. The article
participates in all the existing flows — read state, summaries (if Ollama is
enabled), filters (you can auto-tag newsletters, route them to a board, etc.),
search, digest emails.
