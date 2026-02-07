#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TAIL="100"
FOLLOW="true"
SERVICE="all"
DRONE_PROJECT=""

usage() {
  cat <<EOF
Usage:
  ./scripts/logs.sh [--tail N] [--no-follow] [service]
  ./scripts/logs.sh --drone <project> [--tail N] [--no-follow]

Examples:
  ./scripts/logs.sh
  ./scripts/logs.sh gateway
  ./scripts/logs.sh --drone chartly-drone-abc12345
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tail) TAIL="$2"; shift 2;;
    --no-follow) FOLLOW="false"; shift 1;;
    --drone) DRONE_PROJECT="$2"; shift 2;;
    -h|--help) usage; exit 0;;
    *) SERVICE="$1"; shift 1;;
  esac
done

if ! command -v docker >/dev/null 2>&1; then
  echo "docker not found" >&2
  exit 1
fi

args=(logs --tail "$TAIL")
if [[ "$FOLLOW" == "true" ]]; then args=(-f "${args[@]}"); fi

if [[ -n "$DRONE_PROJECT" ]]; then
  echo "[INFO] Tailing drone logs (project=$DRONE_PROJECT)..."
  docker compose -p "$DRONE_PROJECT" -f docker-compose.drone.yml "${args[@]}"
  exit $?
fi

echo "[INFO] Tailing control-plane logs..."
if [[ "$SERVICE" == "all" ]]; then
  docker compose -f docker-compose.control.yml "${args[@]}"
else
  docker compose -f docker-compose.control.yml "${args[@]}" "$SERVICE"
fi