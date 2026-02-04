#!/usr/bin/env bash
set -euo pipefail

# Chartly 2.0  stop_all.sh
# Cross-platform helper: stops the local stack (docker compose).

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"

say() { printf '[stop_all] %s\n' "$*"; }

say "repo_root: $repo_root"

# Find compose file (best-effort)
compose_file=""
if [[ -f "$repo_root/docker-compose.yml" ]]; then
  compose_file="$repo_root/docker-compose.yml"
elif [[ -f "$repo_root/docker/compose.yml" ]]; then
  compose_file="$repo_root/docker/compose.yml"
fi

if command -v docker >/dev/null 2>&1; then
  if [[ -n "$compose_file" ]]; then
    say "Stopping docker compose: $compose_file"
    docker compose -f "$compose_file" down

    say "docker compose ps (post-down):"
    docker compose -f "$compose_file" ps || true
  else
    say "No docker compose file found (docker-compose.yml or docker/compose.yml). Nothing to stop."
  fi
else
  say "docker not found. Nothing to stop."
fi

say "done"
