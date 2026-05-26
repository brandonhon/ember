# Security Policy

## Supported versions

Ember has not cut a 1.0 release yet. Security fixes land on `main`. There is no LTS branch.

## Reporting a vulnerability

Please **do not open a public GitHub issue for security vulnerabilities.**

Instead, use one of:

1. **GitHub Security Advisories** (preferred): open a private security advisory at
   <https://github.com/brandonhon/ember/security/advisories/new>.
2. **Direct email** to the maintainer (see commit signatures for the address).

Include:

- A description of the issue and the security impact.
- Step-by-step reproduction (a minimal `curl` or HTTP request is ideal).
- The commit SHA you reproduced against.

You will receive an acknowledgement within **7 days**. Most issues get a patch within **30 days**; complex issues may take longer and we'll keep you updated.

We will credit you in the release notes unless you ask us not to.

## Threat model

Ember is designed for self-hosted, **trusted-network or single-user public** deployment behind a reverse proxy (Caddy in our reference deployment). Specifically:

- All `/api/*` endpoints except `POST /api/auth/login` require authentication.
- Admin-only endpoints additionally require `is_admin = 1`.
- TLS is terminated by the reverse proxy, not by Ember.
- Outbound URL fetches (feed subscription, OPML import, readability enrichment) are blocked from reaching RFC1918, loopback, link-local, and IPv6 ULA / link-local addresses unless `EMBER_ALLOW_PRIVATE_URLS=1` is set.
- Passwords use argon2id with parameters that meet OWASP's 2024 recommendations.

If your deployment differs significantly from this (no reverse proxy, public multi-tenant signup, etc.) please audit the relevant surfaces yourself. Ember is **not** designed for untrusted multi-tenant operation.

## Hardening checklist for self-hosters

1. Set a strong `EMBER_ADMIN_PASSWORD` (40+ chars, random).
2. Generate `EMBER_SESSION_KEY` with `openssl rand -base64 48`.
3. Run behind Caddy/Nginx/Cloudflare — Ember's `:8080` should never be directly exposed.
4. Set firewall rules to restrict the Ollama port (`:11434`) to localhost or the docker network.
5. Keep the `qwen2.5:0.5b` (or your chosen model) updated via Settings → Language Model.
6. Schedule DB backups in Settings → Database. Keep at least 7 backups.
7. Don't enable `EMBER_ALLOW_PRIVATE_URLS` unless you trust every user who can add feeds.
8. Run `make vulncheck` periodically to catch upstream CVEs.
