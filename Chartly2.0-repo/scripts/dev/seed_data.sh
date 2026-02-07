#!/usr/bin/env bash
set -euo pipefail

# Chartly 2.0  seed_data.sh
# Dev-only seeding helper. Requires explicit opt-in.

say() { printf '[seed_data] %s\n' "$*"; }
die() { printf '[seed_data] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage:
  ./seed_data.sh --yes [--base-url <url>] [--seed <name>] [--count <n>]

Defaults:
  --base-url  http://localhost:8080   (or $CHARTLY_BASE_URL)
  --seed      default
  --count     100

Env:
  CHARTLY_BASE_URL   Optional base URL
  CHARTLY_TENANT_ID  Optional tenant id (included in seed body)

Endpoint:
  POST /v1/dev/seed
  Body: { "tenant_id": "...", "seed": "default", "count": 100 }

EOF
}

yes="0"
base_url="${CHARTLY_BASE_URL:-http://localhost:8080}"
seed="default"
count="100"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes) yes="1"; shift ;;
    --base-url) base_url="${2:-}"; shift 2 ;;
    --seed) seed="${2:-}"; shift 2 ;;
    --count) count="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown arg: $1 (use --help)" ;;
  esac
done

if [[ "$yes" != "1" ]]; then
  usage
  die "Refusing to run without --yes"
fi

command -v curl >/dev/null 2>&1 || die "curl not found"

base_url="${base_url%/}"
[[ -n "$base_url" ]] || die "--base-url is required"

# Safety: refuse to run against obvious production URLs unless explicitly allowed.
# (Heuristic only; keeps dev scripts from accidental misuse.)
if echo "$base_url" | grep -Eqi '(prod|production)\.'; then
  die "Refusing to seed against a URL that looks like production: $base_url"
fi

# Validate count as int
if ! echo "$count" | grep -Eq '^[0-9]+$'; then
  die "--count must be an integer"
fi

tenant_id="${CHARTLY_TENANT_ID:-}"

url="$base_url/v1/dev/seed"

# Build JSON body (no jq dependency)
if [[ -n "$tenant_id" ]]; then
  body="{\"tenant_id\":\"$tenant_id\",\"seed\":\"$seed\",\"count\":$count}"
else
  body="{\"seed\":\"$seed\",\"count\":$count}"
fi

say "POST $url"
say "seed=$seed count=$count tenant_id=${tenant_id:-<none>}"

# Use a bounded timeout; show response; fail on non-2xx
curl -fsS --max-time 30 \
  -H "content-type: application/json" \
  -d "$body" \
  "$url" | sed -e 's/^/[seed_data]   /'

say "done"
