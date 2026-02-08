#!/usr/bin/env bash
set -euo pipefail

BASE="${BASE_URL:-http://localhost:8090}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.control.yml}"

pass() { printf "PASS: %s\n" "$1"; }
fail() { printf "FAIL: %s\n" "$1"; return 1; }

check_code() {
  local url="$1"
  local code
  code="$(curl -sS -o /dev/null -w "%{http_code}" "$url" || true)"
  if [[ "$code" =~ ^2 ]]; then
    pass "$url ($code)"
  else
    fail "$url ($code)"
  fi
}

echo "== VERIFY =="

echo "-- /api/events (SSE) --"
if curl -sN --max-time 5 "$BASE/api/events" | head -n 5 | rg -q "event:|data:"; then
  pass "/api/events stream"
else
  fail "/api/events stream"
fi

echo "-- /api/crypto/symbols --"
check_code "$BASE/api/crypto/symbols"

echo "-- /api/reports (GET) --"
check_code "$BASE/api/reports"

echo "-- /api/reports (POST->GET) --"
RID="$(curl -sS -X POST "$BASE/api/reports" -H "Content-Type: application/json" -d '{"profiles":["crypto-watchlist"],"mode":"auto"}' | jq -r '.id // .report_id // empty')"
if [[ -z "$RID" ]]; then
  fail "report create"
else
  pass "report create ($RID)"
  check_code "$BASE/api/reports/$RID"
fi

echo "-- /api/results --"
check_code "$BASE/api/results?limit=1"

echo "-- /api/summary --"
check_code "$BASE/api/summary"

echo "== DONE =="

if [[ "${1:-}" == "--logs" ]]; then
  echo "-- gateway logs (last 50) --"
  docker compose -f "$COMPOSE_FILE" logs --tail=50 gateway || true
  if docker compose -f "$COMPOSE_FILE" ps --format json | rg -q "crypto-stream"; then
    echo "-- crypto-stream logs (last 50) --"
    docker compose -f "$COMPOSE_FILE" logs --tail=50 crypto-stream || true
  fi
fi
