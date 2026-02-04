#!/bin/sh
set -eu

# Chartly 2.0  smoke_test.sh
# Minimal smoke check using ONLY: POSIX sh builtins + curl.

say()  { echo "[smoke] $*"; }
pass() { echo "[smoke] PASS: $*"; }
fail() { echo "[smoke] FAIL: $*"; }
die()  { echo "[smoke] ERROR: $*" 1>&2; exit 1; }

usage() {
  echo "Usage:"
  echo "  ./smoke_test.sh [--base-url <url>] [--timeout <sec>] [--tenant-id <id>] [--request-id <id>]"
  echo ""
  echo "Defaults:"
  echo "  --base-url   $CHARTLY_BASE_URL or http://localhost:8080"
  echo "  --timeout    $CHARTLY_TIMEOUT_SEC or 10"
  echo "  --tenant-id  $CHARTLY_TENANT_ID"
  echo "  --request-id $CHARTLY_REQUEST_ID or auto (pid)"
}

base_url="${CHARTLY_BASE_URL:-http://localhost:8080}"
timeout="${CHARTLY_TIMEOUT_SEC:-10}"
tenant_id="${CHARTLY_TENANT_ID:-}"
request_id="${CHARTLY_REQUEST_ID:-}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --base-url) base_url="${2:-}"; shift 2 ;;
    --timeout) timeout="${2:-}"; shift 2 ;;
    --tenant-id) tenant_id="${2:-}"; shift 2 ;;
    --request-id) request_id="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown arg: $1 (use --help)" ;;
  esac
done

# normalize base_url (strip trailing slashes)
while :; do
  case "$base_url" in
    */) base_url="${base_url%/}" ;;
    *) break ;;
  esac
done
[ -n "$base_url" ] || die "--base-url required"

# validate timeout integer
case "$timeout" in
  ''|*[!0-9]*) die "--timeout must be integer seconds" ;;
esac
[ "$timeout" -ge 1 ] || die "--timeout must be >= 1"

# Generate request_id if missing
if [ -z "$request_id" ]; then
  request_id="smoke-$$"
fi

# Optional guard for tenant id (since we may interpolate into header)
if [ -n "$tenant_id" ]; then
  case "$tenant_id" in
    *[!A-Za-z0-9._-]*) die "--tenant-id has unsupported chars (allowed: A-Z a-z 0-9 . _ -)" ;;
  esac
fi

traceparent="00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"

tests="0"
failures="0"

is_2xx() {
  case "$1" in
    2??) return 0 ;;
    *) return 1 ;;
  esac
}

last_url=""
last_code=""
last_rc="0"

http_get() {
  path="$1"
  last_url="$base_url$path"
  last_code=""
  last_rc="0"

  if [ -n "$tenant_id" ]; then
    last_code="$(curl -sS --max-time "$timeout" -o /dev/null -w "%{http_code}" \
      -X GET \
      -H "accept: application/json" \
      -H "traceparent: $traceparent" \
      -H "x-request-id: $request_id" \
      -H "x-chartly-tenant: $tenant_id" \
      "$last_url")" || last_rc="$?"
  else
    last_code="$(curl -sS --max-time "$timeout" -o /dev/null -w "%{http_code}" \
      -X GET \
      -H "accept: application/json" \
      -H "traceparent: $traceparent" \
      -H "x-request-id: $request_id" \
      "$last_url")" || last_rc="$?"
  fi
}

run() {
  name="$1"
  path="$2"

  tests=$((tests + 1))
  say "TEST: $name (GET $path)"

  last_rc="0"
  http_get "$path"

  if [ "$last_rc" != "0" ]; then
    failures=$((failures + 1))
    fail "$name (curl error)"
    say "  url: $last_url"
    say "  curl_exit: $last_rc"
    return 1
  fi

  if is_2xx "$last_code"; then
    pass "$name"
    return 0
  fi

  failures=$((failures + 1))
  fail "$name (http $last_code)"
  say "  url: $last_url"
  return 1
}

say "base_url:   $base_url"
say "timeout:    ${timeout}s"
say "tenant_id:  ${tenant_id:-<none>}"
say "request_id: $request_id"
say "traceparent:$traceparent"

run "health" "/health"
run "ready"  "/ready"

passes=$((tests - failures))
say "SUMMARY: tests=$tests pass=$passes fail=$failures"

if [ "$failures" -ne 0 ]; then
  exit 1
fi
exit 0
