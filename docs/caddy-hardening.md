# Hardening Caddy

Ember ships with a working [`deploy/Caddyfile`](https://github.com/brandonhon/ember/blob/main/deploy/Caddyfile)
that terminates TLS and reverse-proxies to the app. It's a sane baseline, not a
locked-down edge. This page covers the hardening worth adding when Ember faces
the public internet.

## Division of responsibility

Caddy and Ember each own part of the security surface. Knowing the split keeps
you from duplicating (or worse, contradicting) headers.

**Ember sets on every response** (including 404/405 and errors), so you do **not**
need to add these at the proxy:

- `Content-Security-Policy` (locked down — see [Security](/security#transport-proxy-expectations))
- `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Permissions-Policy`, COOP, CORP
- HSTS — but **only when the request is HTTPS** (direct TLS, or a trusted proxy
  reporting `X-Forwarded-Proto: https`)

**Caddy owns the edge:** certificate acquisition and renewal, TLS protocol/cipher
selection, the HTTP→HTTPS redirect, request/connection limits, and access logging.

Because Ember already emits the content-security headers, the proxy's job is the
transport, not re-stamping headers. The one header it makes sense to assert at the
edge is HSTS (the bundled Caddyfile already does), so it's present even on responses
Caddy generates itself (e.g. a TLS-level error page before the request reaches Ember).

## Trust the proxy correctly

This is the single most important step, and it's easy to get wrong in both
directions.

Ember honors `X-Real-IP` (rate-limit keying) and `X-Forwarded-Proto` (HTTPS
detection for HSTS and `Secure` cookies) **only** from peers listed in
`EMBER_TRUSTED_PROXIES`. The default is **empty — trust nobody**.

- If `EMBER_TRUSTED_PROXIES` is **unset** behind Caddy, Ember treats itself as the
  edge: every request looks like it comes from Caddy's bridge IP (so the rate
  limiter keys all clients to one bucket) and `X-Forwarded-Proto` is ignored.
- If it's set **too broadly** (e.g. `0.0.0.0/0`), any peer that can reach Ember
  directly can forge `X-Real-IP` to evade the limiter or poison logs.

Set it to the proxy's address/range and nothing more. The bundled compose uses the
Docker user-defined bridge range:

```yaml
# deploy/docker-compose.yml — ember service
EMBER_TRUSTED_PROXIES: ${EMBER_TRUSTED_PROXIES:-172.16.0.0/12}
```

On Caddy's side, tell Caddy which upstream hops to trust when it determines the
real client IP (Caddy 2.7+):

```
{
	servers {
		trusted_proxies static private_ranges
	}
}
```

`private_ranges` is a built-in shorthand for the RFC1918 + loopback + CGNAT ranges.
If Caddy is itself behind another load balancer or a CDN (Cloudflare), list **that**
hop's ranges instead — otherwise Caddy will report the LB's IP as the client.

## Lock down the admin API

Caddy exposes a local admin API on `localhost:2019` by default. It can change the
running config, including TLS and routing. In a container it's only reachable from
inside the container, but if you don't use it, turn it off:

```
{
	admin off
}
```

If you do need it (e.g. for `caddy reload`), bind it to a unix socket or keep it on
loopback and never publish port `2019`.

## TLS

Caddy's defaults are already strong: TLS 1.2 + 1.3 only, a modern cipher suite list,
automatic OCSP stapling, and automatic certificate renewal. You rarely need to touch
them. If a compliance baseline requires TLS 1.3 only:

```
{$EMBER_HOSTNAME:localhost} {
	tls {
		protocols tls1.3
	}
	# ...
}
```

Be deliberate: TLS-1.3-only excludes older clients. For most self-hosted setups the
default (1.2+) is the right call.

**Certificates.** Public DNS name + reachable ports 80/443 + a valid `CADDY_EMAIL`
→ Caddy fetches a free Let's Encrypt cert automatically. For a homelab with no public
name, use Caddy's internal CA and trust it on your clients:

```
{$EMBER_HOSTNAME:localhost} {
	tls internal
	# ...
}
```

then run `caddy trust` on each client. (This line is already present, commented, in
the bundled Caddyfile.)

**HSTS preload.** The bundled config sends `max-age=31536000; includeSubDomains`.
Only add `; preload` and submit to [hstspreload.org](https://hstspreload.org) once
you're certain **every** subdomain of the apex will always be HTTPS — preload is
hard to undo and applies to the whole domain.

## Limit request size and slow-client exposure

Ember has its own per-route rate limiting, but the proxy can shed abuse before it
reaches the app. Cap the request body (Ember has no endpoint that needs large
uploads beyond OPML/TT-RSS imports — a few MB is plenty) and set read/write
timeouts to blunt slow-loris:

```
{
	servers {
		timeouts {
			read_body   10s
			read_header 5s
			idle        2m
		}
		max_header_size 16KB
	}
}

{$EMBER_HOSTNAME:localhost} {
	request_body {
		max_size 12MB
	}
	# ...
}
```

Tune `max_size` up if you import very large OPML/TT-RSS exports.

> **Rate limiting at the edge** needs the third-party
> [`caddy-ratelimit`](https://github.com/mholt/caddy-ratelimit) plugin, which isn't
> in the stock `caddy:2-alpine` image — you'd build a custom image with
> `xcaddy`. Ember's built-in limiter already covers login, feed-add, and search,
> so an edge limiter is optional defense-in-depth, not a requirement.

## Reduce information disclosure

Caddy advertises itself with a `Server: Caddy` header. Strip it if you'd rather not
name the proxy:

```
{$EMBER_HOSTNAME:localhost} {
	header -Server
	# ...
}
```

Avoid Caddy's `debug` global option in production — it logs verbosely, including
header values.

## Access logging

The stock config doesn't log requests. Turn on structured access logs so you can
spot scans and abuse, and keep them out of the container's stdout if you ship logs
elsewhere:

```
{$EMBER_HOSTNAME:localhost} {
	log {
		output file /var/log/caddy/access.log {
			roll_size 10MB
			roll_keep 10
		}
		format json
	}
	# ...
}
```

Mount a writable volume for `/var/log/caddy` if you use a file output. Caddy
redacts `Authorization` and cookie values by default; double-check before widening
the log format.

## Container hardening

Defenses that live in `docker-compose.yml`, not the Caddyfile:

```yaml
caddy:
  image: caddy:2-alpine
  read_only: true                 # Caddyfile is mounted :ro; data/config are named volumes
  cap_drop: [ALL]
  cap_add: [NET_BIND_SERVICE]     # needed to bind 80/443
  security_opt:
    - no-new-privileges:true
  tmpfs:
    - /tmp
```

- `read_only` + `tmpfs:/tmp` keeps the container filesystem immutable; the
  `caddy-data` / `caddy-config` named volumes remain writable for certs.
- `cap_drop: ALL` then re-add only `NET_BIND_SERVICE` so Caddy can bind the
  privileged ports without running as root.
- Keep ports `80`/`443` published but **never** publish the admin port `2019`.

## A hardened Caddyfile, end to end

Putting the proxy-side pieces together (container hardening stays in compose):

```
{
	email {$CADDY_EMAIL:admin@localhost}
	admin off
	{$EMBER_HTTPS_REDIRECT_DIRECTIVE:}
	servers {
		trusted_proxies static private_ranges
		timeouts {
			read_body   10s
			read_header 5s
			idle        2m
		}
		max_header_size 16KB
	}
}

{$EMBER_HOSTNAME:localhost} {
	encode zstd gzip
	header {
		Strict-Transport-Security "max-age=31536000; includeSubDomains"
		-Server
	}
	request_body {
		max_size 12MB
	}
	log {
		output file /var/log/caddy/access.log {
			roll_size 10MB
			roll_keep 10
		}
		format json
	}
	reverse_proxy ember:8080 {
		header_up Host {host}
		header_up X-Real-IP {remote_host}
		header_up X-Forwarded-For {remote_host}
		header_up X-Forwarded-Proto {scheme}
	}
}
```

And the matching ember-side requirement, regardless of which extras you adopt:

```yaml
# ember service
EMBER_TRUSTED_PROXIES: 172.16.0.0/12   # the Caddy↔ember bridge range
```

## Verify

After reloading Caddy, confirm the edge behaves:

```sh
# Headers come back over HTTPS, HSTS present, Server stripped
curl -sI https://your-host/ | grep -iE 'strict-transport|content-security|x-frame|server'

# HTTP redirects to HTTPS (unless you disabled the redirect)
curl -sI http://your-host/ | grep -i location

# Admin port is not reachable from outside the container
curl -s --max-time 3 http://your-host:2019/config/ || echo "admin closed (good)"
```

You should see HSTS and Ember's CSP/`X-Frame-Options` on the HTTPS response, a
`301` to HTTPS on plain HTTP, and no answer on `:2019`. Check the rate limiter keys
on real client IPs by confirming `EMBER_TRUSTED_PROXIES` is set — if every request
in the logs shows the Caddy bridge IP, it isn't.
