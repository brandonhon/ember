# Upgrading

Ember ships as tagged releases (`vX.Y.Z`). Upgrading means running the newer build — there is no separate migration step to run by hand: schema migrations are embedded in the binary and applied automatically at startup.

The two things to do every time: **read the release notes** and **back up your database first**.

## Before you upgrade

1. **Read the [release notes](https://github.com/brandonhon/ember/releases)** for every version between yours and the target. Note anything about new or renamed environment variables, or behavior changes.
2. **Back up the database.** The easiest path is **Settings → Database → Backup now** (writes a copy under `/data/exports/`, retained per `db_backup_keep`). Or snapshot the volume directly:

   ```bash
   docker run --rm -v ember-data:/data -v "$PWD:/backup" alpine \
     cp /data/ember.db /backup/ember-$(date +%Y%m%d).db
   ```

   This matters because migrations are **forward-only** — there is no automatic downgrade. A backup is your rollback path (see [Rolling back](#rolling-back)).

## Docker Compose (recommended)

If you pin a specific tag in `deploy/docker-compose.yml`, bump it to the new version first:

```yaml
    # deploy/docker-compose.yml — the ember service
-    image: ghcr.io/brandonhon/ember:v0.8.7
+    image: ghcr.io/brandonhon/ember:v0.8.8
```

Then pull and restart just the `ember` service:

```bash
docker compose pull ember     # fetch the new image
docker compose up -d ember    # recreate the container on the new image
docker compose logs -f ember  # watch migrations run on first boot
```

If you track a floating tag (`:latest`, `:0`, `:0.8`) instead of an immutable `vX.Y.Z`, you don't edit the file — `docker compose pull ember` fetches whatever that tag now points at. Pinning `vX.Y.Z` is recommended for production so upgrades only happen when you choose. See the [image tag table](/getting-started#run-from-the-released-container-image) for what each tag tracks.

Only the `ember` container restarts; Caddy and Ollama keep running. Expect a few seconds of downtime while the container recreates and migrations apply.

## Released binary

If you run the binary directly (not Compose), grab the new release artifact, verify it, and swap it in:

```bash
VER=v0.8.8
base="https://github.com/brandonhon/ember/releases/download/$VER"
curl -fLO "$base/ember-$VER-linux-amd64.tar.gz"
curl -fLO "$base/SHA256SUMS"
sha256sum --ignore-missing -c SHA256SUMS    # verify the download
tar xzf "ember-$VER-linux-amd64.tar.gz"
# stop the service, replace the binary, start it again
sudo systemctl stop ember
sudo install ember /usr/local/bin/ember
sudo systemctl start ember
```

Migrations run on the next start, against your existing `EMBER_DB_PATH`.

## How migrations work

- Embedded in the binary and applied **automatically at startup** — nothing to run manually.
- **Append-only and sequential.** You can skip intermediate versions safely: upgrading directly from, say, `v0.8.3` to `v0.8.8` applies every migration in between in order on first boot.
- Some upgrades also kick off **idempotent background backfills** (e.g. cross-feed dedup keys). These run in a goroutine and don't block startup; the app is usable immediately and the backfill completes on its own.

## Verify the upgrade

```bash
docker compose exec ember /ember version        # → ember v0.8.8
curl -fsS https://your-host/healthz             # → ok
```

Or sign in and check **Settings → About** (the version is also returned by `GET /api/me`). Tail the logs once to confirm there were no migration errors:

```bash
docker compose logs ember | grep -iE "migrat|error|panic"
```

## Rolling back

Because migrations are forward-only, **do not** simply point the old image at a database a newer version has migrated — the schema may have moved ahead of what the old binary expects. To roll back:

1. Stop the container.
2. Restore the pre-upgrade database backup (replace `/data/ember.db` with the copy you made above).
3. Start the **previous** image tag.

If you didn't take a backup and only need to undo a small change, restore from the most recent **Settings → Database** backup under `/data/exports/`.

## Notes

- **Pin in production.** Use an immutable `vX.Y.Z` tag so a `docker compose pull` is a deliberate, reviewed upgrade rather than a surprise.
- **Single instance.** Ember is one process over one SQLite file; there's no rolling/zero-downtime upgrade — the brief restart is expected.
- **Config drift.** If a release adds or renames an env var, update your `.env` before restarting. Required vars are listed in [Configuration](/configuration#required-env-vars).
