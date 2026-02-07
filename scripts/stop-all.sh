#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PREFIX="${DRONE_PROJECT_PREFIX:-chartly-drone-}"

echo "[INFO] Stopping control plane..."
docker compose -f docker-compose.control.yml down --remove-orphans || true

echo "[INFO] Stopping default drone project (best-effort)..."
docker compose -f docker-compose.drone.yml down --remove-orphans || true

echo "[INFO] Stopping drone projects (prefix=$PREFIX)..."
projects="$(docker ps --format '{{.Names}}' | grep -oE "${PREFIX}[a-z0-9\-]+" | sort -u || true)"
if [[ -z "$projects" ]]; then
  echo "[INFO] No running drone containers matched."
  exit 0
fi

while IFS= read -r proj; do
  [[ -z "$proj" ]] && continue
  echo "[INFO] Stopping drone compose project: $proj"
  docker compose -p "$proj" -f docker-compose.drone.yml down --remove-orphans || true
done <<< "$projects"

echo "[INFO] Stop complete."