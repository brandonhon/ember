#!/usr/bin/env bash
# Fail if a container image this repo builds on or ships is not pinned by digest.
#
#   scripts/check-pins.sh            # or: make check-pins
#
# Digest pins are what make a rebuild of an old commit reproduce the image that
# commit originally produced, and what lets a CVE advisory be answered by
# reading the repo instead of asking a deployment. Nothing else notices when one
# goes missing: dropping an `@sha256:` builds fine and stays green until someone
# needs the guarantee. Hence this check.
#
# Not checked here: whether a digest is the multi-arch *index* rather than one
# platform's manifest. That needs the registry, and a per-platform digest only
# fails on the linux/arm64 leg of a release — see CONTRIBUTING.md for the buildx
# command that catches it.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

fail=0
ok=0

report() { # $1=status  $2=file:line  $3=ref  [$4=note]
  case "$1" in
    ok)   ok=$((ok + 1));   printf '  ✓ %-34s %s\n' "$2" "$3" ;;
    skip) printf '  – %-34s %s (%s)\n' "$2" "$3" "${4:-skipped}" ;;
    bad)  fail=$((fail + 1)); printf '  ✗ %-34s %s — NOT pinned by digest\n' "$2" "$3" ;;
  esac
}

# --- Dockerfiles ----------------------------------------------------------
# Every FROM must carry @sha256:, except one naming an earlier build stage
# (`FROM build AS x`), which refers to this file and has no digest to pin.
for df in Dockerfile Dockerfile.release; do
  [ -f "$df" ] || { echo "error: $df not found" >&2; exit 1; }
  echo "$df"
  stages=" "
  while IFS=$'\t' read -r lineno ref stage; do
    if [[ "$stages" == *" $ref "* ]]; then
      report skip "$df:$lineno" "$ref" "build stage"
    elif [[ "$ref" == *"@sha256:"* ]]; then
      report ok "$df:$lineno" "$ref"
    else
      report bad "$df:$lineno" "$ref"
    fi
    [ -n "$stage" ] && stages+="$stage "
  done < <(awk '
    toupper($1) == "FROM" {
      ref = ""; stage = ""
      for (i = 2; i <= NF; i++) {
        if ($i ~ /^--/) continue            # --platform=... and friends
        if (ref == "") { ref = $i; continue }
        if (toupper($i) == "AS" && i < NF) { stage = $(i + 1); break }
      }
      if (ref != "") print NR "\t" ref "\t" stage
    }' "$df")
done

# --- Compose --------------------------------------------------------------
# Images deliberately left floating. Each needs a reason, not just an entry.
#   ollama/ollama:latest — optional local LLM, tracks upstream by design
#   ember:sandbox        — built locally by `make sandbox`, never pulled
unpinned_ok='^(ollama/ollama:latest|ember:sandbox)$'

for cf in deploy/docker-compose*.yml; do
  [ -f "$cf" ] || continue
  mapfile -t rows < <(grep -nE '^[[:space:]]*image:[[:space:]]*\S+' "$cf" || true)
  [ ${#rows[@]} -eq 0 ] && continue
  echo "$cf"
  for row in "${rows[@]}"; do
    lineno="${row%%:*}"
    ref="$(printf '%s' "${row#*:}" | sed -E 's/^[[:space:]]*image:[[:space:]]*//; s/[[:space:]]*(#.*)?$//')"
    if [[ "$ref" =~ $unpinned_ok ]]; then
      report skip "$cf:$lineno" "$ref" "floating on purpose"
    elif [[ "$ref" == *"@sha256:"* ]]; then
      report ok "$cf:$lineno" "$ref"
    else
      report bad "$cf:$lineno" "$ref"
    fi
  done
done

echo
if [ "$fail" -gt 0 ]; then
  echo "check-pins: $fail image(s) not pinned by digest, $ok pinned" >&2
  echo "Pin it as name:tag@sha256:… — resolve the digest with:" >&2
  echo "  docker buildx imagetools inspect <image>:<tag> | head -3" >&2
  echo "and keep the tag alongside so the version stays readable." >&2
  exit 1
fi
echo "check-pins: $ok image(s) pinned by digest"
