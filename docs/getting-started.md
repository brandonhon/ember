# Getting started

Ember runs as a Docker stack (`ember` + `caddy` + `ollama`) or as a standalone Go binary if you'd rather host Ollama elsewhere.

## Install options

| Option | When to use |
| --- | --- |
| **Pre-built container** (`ghcr.io/brandonhon/ember:vX.Y.Z`, also `:latest`, `:X`, `:X.Y`) | You want a stable release without a local build step. Multi-arch (`linux/amd64`, `linux/arm64`). |
| **Pre-built binary** ([Releases](https://github.com/brandonhon/ember/releases)) | You're running outside Docker — bare metal, VM, systemd, NAS. Tarballs for `linux-{amd64,arm64}` and `darwin-{amd64,arm64}`. Verify via the bundled `SHA256SUMS`. |
| **Build from source** (`make embed build`) | You're contributing, or you need a tip-of-main change. See [Build from source](#build-from-source). |

The default `deploy/docker-compose.yml` still builds locally from the `Dockerfile` so a fresh clone Just Works. To switch to the released image, replace the `ember` service's `build:` block with `image: ghcr.io/brandonhon/ember:latest`.

## Prerequisites

- Docker + docker-compose (or Podman) for the stack flow.
- Go 1.26+ if building from source.
- A domain or localhost host pointing at your machine.

## Stack install (recommended)

```sh
git clone https://github.com/brandonhon/ember.git
cd ember/deploy
cp .env.example .env
```

Edit `.env`. The two required vars:

```sh
# 32+ bytes of random data. Used to sign session cookies.
EMBER_SESSION_KEY=$(openssl rand -base64 48)

# First-run admin password. Change it after first login.
EMBER_ADMIN_PASSWORD=<your-strong-password>
```

Bring the stack up:

```sh
docker compose up -d
```

Three containers start:

| Container | Purpose |
| --- | --- |
| `ember-caddy-1` | TLS termination, reverse proxy, serves the SPA + `/api`. |
| `ember-ember-1` | Ember itself: API, poller, summary worker. |
| `ember-ollama-1` | Local LLM (default model `qwen2.5:0.5b`, ~400 MB). |

Visit `https://localhost` (accept the self-signed cert in dev). Log in with `admin` + the password you just set. You'll land on an onboarding panel — pick a starter pack and you're off.

> If ports 80/443 are already taken on your machine, set `EMBER_HTTP_PORT` and `EMBER_HTTPS_PORT` in `.env` before `docker compose up -d` and reach the site at the mapped port — e.g. `EMBER_HTTPS_PORT=8443` → `https://localhost:8443`. See [Configuration → Stack-level env vars](/configuration#stack-level-env-vars-docker-compose) for caveats around Let's Encrypt.

## First-run checklist

1. Log in as the admin you created in `.env`.
2. Open **Settings → Language model** and confirm the recommendation matches your hardware. The `ember probe` subcommand (or that section) reports detected RAM/CPU/GPU and the suggested model.
3. Open **Settings → Database**, schedule a daily backup, and pick a cleanup cadence.
4. Open **Settings → Preferences** and pick your theme + article density.
5. Click **Browse starter packs** or paste a feed URL — or a homepage URL — into the sidebar "+ Add feed". Ember auto-discovers the feed link.
6. (Optional) **Settings → Passkeys** to register a passkey for password-less sign-in. Requires `EMBER_PUBLIC_URL` to be set.
7. (Optional) Configure SMTP env vars (see [Configuration](/configuration#optional-env-vars)) and enable a daily digest email from your profile.
8. (Optional) Install Ember as a PWA — Chrome / Edge / Safari "Install app" menu. Once installed, new articles trigger an OS-level numeric badge on the app icon (taskbar / dock / launcher) in addition to the in-tab favicon dot.

## Run from a pre-built binary

Grab a tarball from [Releases](https://github.com/brandonhon/ember/releases) — pick the asset matching your OS + arch:

```sh
VERSION=v0.6.0   # or 'latest' redirect: /releases/latest/download/...
curl -L -o ember.tar.gz \
  "https://github.com/brandonhon/ember/releases/download/${VERSION}/ember-${VERSION}-linux-amd64.tar.gz"

# Verify against SHA256SUMS shipped on the same release
curl -L -o SHA256SUMS \
  "https://github.com/brandonhon/ember/releases/download/${VERSION}/SHA256SUMS"
shasum -a 256 -c SHA256SUMS --ignore-missing

tar -xzf ember.tar.gz
./ember --version   # should print the tag you downloaded
```

Then set the env vars from [Configuration](/configuration#required-env-vars) and run `./ember`. Front it with Caddy / Nginx / Cloudflare for TLS.

## Build from source

If you don't want the docker stack:

```sh
make web-install        # one-time
make embed build        # produces ./bin/ember (~25 MB)
```

Set the required env vars in your shell or systemd unit:

```sh
export EMBER_ADDR=:8080
export EMBER_DB_PATH=/var/lib/ember/ember.db
export EMBER_SESSION_KEY=...
export EMBER_ADMIN_USER=admin
export EMBER_ADMIN_PASSWORD=...
export EMBER_OLLAMA_URL=http://127.0.0.1:11434   # or wherever Ollama lives
./bin/ember
```

Put Caddy / Nginx / Cloudflare in front to terminate TLS.

## Local development

```sh
# In one terminal — hot-reload SPA
cd web && npm run dev

# In another — ember in test mode (seeded with fake articles)
EMBER_TEST_MODE=1 ./bin/ember

# Open http://localhost:5173 (Vite proxies /api → :8080)
```

Test mode seeds a deterministic admin (`admin` / `admintest`), one feed, and 12 fixture articles. Useful when poking at UI changes without waiting on real fetches.

## Verifying the install

- `docker compose ps` — all three containers (`ember-caddy-1`, `ember-ember-1`, `ember-ollama-1`) should report `Up` / `healthy`.
- `https://localhost/healthz` — public liveness probe (Caddy uses this).
- `https://localhost/metrics` — admin-authenticated metrics endpoint.

If `/healthz` returns "ok" but the SPA looks broken, check `docker compose logs ember` for migration errors and `docker compose logs caddy` for cert / proxy errors.
