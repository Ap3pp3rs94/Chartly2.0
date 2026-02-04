#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TIMEOUT_SEC="${TIMEOUT_SEC:-90}"
CONTROL_PLANE="${CONTROL_PLANE:-http://localhost}"

printf "%s\n" "Running doctor..."
"$ROOT/scripts/control-plane-doctor.sh"

printf "%s\n" "Bootstrapping..."
"$ROOT/scripts/control-plane-bootstrap.sh"

printf "%s\n" "Building control plane..."
docker compose -f docker-compose.control.yml build

printf "%s\n" "Starting control plane..."
docker compose -f docker-compose.control.yml up -d

printf "%s\n" "Waiting for health: $CONTROL_PLANE/health (timeout=${TIMEOUT_SEC}s)"
elapsed=0
while [[ "$elapsed" -lt "$TIMEOUT_SEC" ]]; do
  if command -v curl >/dev/null 2>&1; then
    if curl -fsS "$CONTROL_PLANE/health" >/dev/null 2>&1; then
      printf "%s\n" " Control plane responding"
      printf "%s\n" "  Gateway:  $CONTROL_PLANE"
      printf "%s\n" "  Health:   $CONTROL_PLANE/health"
      printf "%s\n" "  Status:   $CONTROL_PLANE/api/status"
      exit 0
    fi
  fi
  sleep 2
  elapsed=$((elapsed+2))
  printf "."
done
printf "\n%s\n" " Timeout waiting for control plane"
docker compose -f docker-compose.control.yml ps || true
docker compose -f docker-compose.control.yml logs --tail 200 || true
exit 1