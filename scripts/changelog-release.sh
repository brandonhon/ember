#!/usr/bin/env bash
# Update CHANGELOG.md link references when cutting a release.
#
#   scripts/changelog-release.sh <version>      # e.g. v0.9.3 or 0.9.3
#
# Adds a `[X.Y.Z]:` compare link (from the previous released version to the new
# tag) and bumps the `[Unreleased]:` ref to start from the new tag. The section
# *content* (`## [X.Y.Z] - <date>` + the entries) is hand-written — this only
# maintains the mechanical bottom link refs so they can't be forgotten. The
# release workflow's `prepare` job fails the release if they're missing.
#
# Idempotent: re-running for a version whose ref already exists is a no-op.
set -euo pipefail

ver="${1:?usage: changelog-release.sh <version>  (e.g. v0.9.3)}"
ver="${ver#v}" # normalize: drop a leading v
if [[ ! "$ver" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "error: version must look like X.Y.Z[-pre], got: $1" >&2
  exit 1
fi
tag="v${ver}"

# Resolve CHANGELOG.md relative to the repo root (script lives in scripts/).
root="$(cd "$(dirname "$0")/.." && pwd)"
file="$root/CHANGELOG.md"
[ -f "$file" ] || { echo "error: $file not found" >&2; exit 1; }

if grep -qE "^\[${ver}\]: " "$file"; then
  echo "CHANGELOG: [${ver}] link ref already present — nothing to do"
  exit 0
fi

# The previous released version is whatever [Unreleased] currently compares from
# (".../compare/<PREV>...develop").
prev_line="$(grep -E '^\[Unreleased\]: ' "$file" || true)"
[ -n "$prev_line" ] || { echo "error: no '[Unreleased]:' ref in $file" >&2; exit 1; }
prev="$(printf '%s' "$prev_line" | sed -E 's#.*/compare/##; s#\.\.\..*##')"
[ -n "$prev" ] || { echo "error: could not parse previous version from: $prev_line" >&2; exit 1; }

base="https://github.com/brandonhon/ember"
tmp="$(mktemp)"
awk -v ver="$ver" -v tag="$tag" -v prev="$prev" -v base="$base" '
  /^\[Unreleased\]: / {
    print "[Unreleased]: " base "/compare/" tag "...develop"
    print "[" ver "]: " base "/compare/" prev "..." tag
    next
  }
  { print }
' "$file" > "$tmp"
mv "$tmp" "$file"
echo "CHANGELOG: added [${ver}] (compare ${prev}...${tag}) and bumped [Unreleased] to ${tag}"
