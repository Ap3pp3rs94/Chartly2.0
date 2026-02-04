#!/usr/bin/env bash
set -euo pipefail

# Chartly 2.0  start_all.sh
# Cross-platform helper: starts the local stack (docker compose) and runs a smoke check.

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"

base_url="${CHARTLY_BASE_URL:-http://localhost:8080}"
base_url="${base_url%/}"

say() { printf '[start_all] %s\n' "$*"; }
die() { printf '[start_all] ERROR: %s\n' "$*" >&2; exit 1; }

say "repo_root: $repo_root"
say "base_url:  $base_url"

# Find compose file (best-effort)
compose_file=""
if [[ -f "$repo_root/docker-compose.yml" ]]; then
  compose_file="$repo_root/docker-compose.yml"
elif [[ -f "$repo_root/docker/compose.yml" ]]; then
  compose_file="$repo_root/docker/compose.yml"
fi

if command -v docker >/dev/null 2>&1; then
  if [[ -n "$compose_file" ]]; then
    say "Starting docker compose: $compose_file"
    docker compose -f "$compose_file" up -d
    say "docker compose ps:"
    docker compose -f "$compose_file" ps || true
  else
    say "No docker compose file found (docker-compose.yml or docker/compose.yml). Skipping stack start."
  fi
else
  say "docker not found. Skipping stack start."
fi

# Smoke check via curl (best-effort)
if command -v curl >/dev/null 2>&1; then
  for ep in /health /ready; do
    url="$base_url$ep"
    say "GET $url"
    # Fail if non-2xx or timeout
    curl -fsS --max-time 10 "$url" | sed -e 's/^/[start_all]   /'
  done
  say "smoke check: OK"
else
  say "curl not found. Skipping smoke check."
fi

say "done"
