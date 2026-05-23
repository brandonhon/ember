#!/usr/bin/env bash
# deploy/smoke_test.sh — exercises the compose stack end-to-end. Brings the
# stack up, waits for ember to be healthy, hits a handful of endpoints, and
# tears down. Intended for CI; the gate is: every assertion below passes.
#
# Usage:
#   deploy/smoke_test.sh
set -euo pipefail

cd "$(dirname "$0")"

cleanup() {
  docker compose -f docker-compose.yml down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

if [ ! -f .env ]; then
  cp .env.example .env
  # Make the session key 48 bytes of random for the smoke test.
  KEY="$(openssl rand -base64 48 | tr -d '\n')"
  sed -i.bak "s|^EMBER_SESSION_KEY=.*|EMBER_SESSION_KEY=${KEY}|" .env
  sed -i.bak "s|^EMBER_ADMIN_PASSWORD=.*|EMBER_ADMIN_PASSWORD=smoke-test-pw|" .env
  rm -f .env.bak
fi

echo "==> Bringing up the stack"
docker compose -f docker-compose.yml up -d --build

# Wait until ember reports healthy.
echo "==> Waiting for ember healthcheck"
for i in $(seq 1 60); do
  state="$(docker inspect --format='{{.State.Health.Status}}' "$(docker compose -f docker-compose.yml ps -q ember)" 2>/dev/null || true)"
  if [ "$state" = "healthy" ]; then break; fi
  sleep 2
done
if [ "$state" != "healthy" ]; then
  echo "ember never reached healthy: $state" >&2
  docker compose -f docker-compose.yml logs ember | tail -50 >&2
  exit 1
fi

# Find the host that maps to caddy:443. With the dev overlay it'd be 8443;
# for the canonical compose we route through caddy on 443.
EMBER_URL="${EMBER_URL:-https://localhost}"

CURL="curl -k -s -c /tmp/ember-jar -b /tmp/ember-jar"

echo "==> /api/me anonymous should be 401"
code="$($CURL -o /dev/null -w '%{http_code}' "$EMBER_URL/api/me")"
[ "$code" = "401" ] || { echo "anonymous /api/me = $code" >&2; exit 1; }

echo "==> Login as admin"
$CURL -X POST -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"smoke-test-pw"}' \
  "$EMBER_URL/api/auth/login" >/dev/null

echo "==> /api/me authenticated should be 200"
code="$($CURL -o /dev/null -w '%{http_code}' "$EMBER_URL/api/me")"
[ "$code" = "200" ] || { echo "authed /api/me = $code" >&2; exit 1; }

echo "==> Search endpoint accepts a query"
code="$($CURL -o /dev/null -w '%{http_code}' "$EMBER_URL/api/search?q=ember")"
[ "$code" = "200" ] || { echo "/api/search = $code" >&2; exit 1; }

echo "==> Static SPA reachable through Caddy"
code="$($CURL -o /tmp/ember-index.html -w '%{http_code}' "$EMBER_URL/")"
[ "$code" = "200" ] || { echo "GET / = $code" >&2; exit 1; }
grep -q '<div id="app"' /tmp/ember-index.html || {
  echo "SPA HTML not served at /" >&2; exit 1;
}

echo "OK — smoke test passed."
