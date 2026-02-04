#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

CONTROL_PLANE="${CONTROL_PLANE:-http://host.docker.internal:8090}"
PROCESS_INTERVAL="${PROCESS_INTERVAL:-5m}"
DRONE_ID="${DRONE_ID:-}"
COUNT="${COUNT:-1}"

usage() {
  cat <<EOF
Usage:
  ./scripts/drone-up.sh [--control-plane URL] [--interval 5m] [--drone-id ID] [--count N]

Notes:
  - Uses docker compose -p to isolate multiple drones.
  - CONTROL_PLANE defaults to http://host.docker.internal:8090 (works with extra_hosts host-gateway).
EOF
}

gen_uuid() {
  if command -v uuidgen >/dev/null 2>&1; then uuidgen | tr '[:upper:]' '[:lower:]'; return; fi
  if command -v python3 >/dev/null 2>&1; then python3 - <<'PY'
import uuid; print(str(uuid.uuid4()))
PY
    return
  fi
  if [[ -r /proc/sys/kernel/random/uuid ]]; then cat /proc/sys/kernel/random/uuid; return; fi
  echo "uuidgen not available" >&2
  exit 1
}

short_id() {
  # keep first 8 alnum chars for project id stability
  echo "$1" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9' | cut -c1-8
}

# parse args
while [[ $# -gt 0 ]]; do
  case "$1" in
    --control-plane) CONTROL_PLANE="$2"; shift 2;;
    --interval) PROCESS_INTERVAL="$2"; shift 2;;
    --drone-id) DRONE_ID="$2"; shift 2;;
    --count) COUNT="$2"; shift 2;;
    -h|--help) usage; exit 0;;
    *) echo "Unknown arg: $1" >&2; usage; exit 2;;
  esac
done

if [[ "$COUNT" -lt 1 ]]; then echo "COUNT must be >= 1" >&2; exit 2; fi

echo "Starting drone(s)..."
echo "Control plane: $CONTROL_PLANE"
echo "Interval: $PROCESS_INTERVAL"
echo "Count: $COUNT"
echo ""

for i in $(seq 1 "$COUNT"); do
  id="$DRONE_ID"
  if [[ -z "$id" || "$COUNT" -gt 1 ]]; then
    id="$(gen_uuid)"
  fi
  proj="chartly-drone-$(short_id "$id")"

  export CONTROL_PLANE="$CONTROL_PLANE"
  export DRONE_ID="$id"
  export PROCESS_INTERVAL="$PROCESS_INTERVAL"

  echo " Launching $proj (drone_id=$id)"
  docker compose -p "$proj" -f docker-compose.drone.yml build >/dev/null
  docker compose -p "$proj" -f docker-compose.drone.yml up -d >/dev/null
done

echo ""
echo " Drone(s) started"
echo "Tail logs:"
echo "  docker compose -p <project> -f docker-compose.drone.yml logs -f"