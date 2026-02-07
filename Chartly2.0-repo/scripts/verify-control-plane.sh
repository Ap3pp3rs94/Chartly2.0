#!/usr/bin/env bash
set -euo pipefail

CONTROL_PLANE="${1:-http://localhost:8090}"
CONTROL_PLANE="${CONTROL_PLANE%/}"

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required for verify-control-plane.sh" >&2
  exit 1
fi

echo "Verifying control plane at $CONTROL_PLANE"

echo ""
echo "GET /health"
curl -fsS "$CONTROL_PLANE/health" | sed 's/^/  /'

echo ""
echo "GET /api/status"
curl -fsS "$CONTROL_PLANE/api/status" | sed 's/^/  /' || true

echo ""
echo "GET /api/profiles (first 1)"
curl -fsS "$CONTROL_PLANE/api/profiles" | sed 's/^/  /' | head -n 40 || true

echo ""
echo "GET /api/drones/stats"
curl -fsS "$CONTROL_PLANE/api/drones/stats" | sed 's/^/  /' || true

echo ""
echo "GET /api/results/summary"
curl -fsS "$CONTROL_PLANE/api/results/summary" | sed 's/^/  /' || true

echo ""
echo "GET /api/runs (first 1)"
curl -fsS "$CONTROL_PLANE/api/runs?limit=1" | sed 's/^/  /' || true

echo ""
echo "GET /api/records (first 1)"
curl -fsS "$CONTROL_PLANE/api/records?limit=1" | sed 's/^/  /' || true

echo ""
echo "Done."