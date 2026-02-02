#!/bin/sh
set -eu

# Chartly 2.0  integration_test.sh
# Minimal integration checks using ONLY: POSIX sh builtins + curl.

say()  { echo "[integration] $*"; }
pass() { echo "[integration] PASS: $*"; }
fail() { echo "[integration] FAIL: $*"; }
die()  { echo "[integration] ERROR: $*" 1>&2; exit 1; }

usage() {
  echo "Usage:"
  echo "  ./integration_test.sh [options]"
  echo ""
  echo "Options:"
  echo "  --base-url <url>      Base URL (default: $CHARTLY_BASE_URL or http://localhost:8080)"
  echo "  --timeout <sec>       Curl timeout seconds (default: $CHARTLY_TIMEOUT_SEC or 10)"
  echo "  --tenant-id <id>      Tenant id header (default: $CHARTLY_TENANT_ID)"
  echo "  --request-id <id>     Request id header (default: $CHARTLY_REQUEST_ID or auto)"
  echo "  --seed                POST /v1/dev/seed (REQUIRES --yes)"
  echo "  --seed-name <name>    Seed name (default: default)"
  echo "  --seed-count <n>      Seed count (default: 100)"
  echo "  --events-path <path>  If set, POST a sample event to this path and require 2xx"
  echo "  --yes                 Required for any mutating step (e.g., --seed)"
  echo "  -h, --help            Show help"
  echo ""
  echo "Env:"
  echo "  CHARTLY_BASE_URL, CHARTLY_TIMEOUT_SEC, CHARTLY_TENANT_ID, CHARTLY_REQUEST_ID, CHARTLY_EVENTS_PATH"
}

# Defaults (env overridable)
base_url="${CHARTLY_BASE_URL:-http://localhost:8080}"
timeout="${CHARTLY_TIMEOUT_SEC:-10}"
tenant_id="${CHARTLY_TENANT_ID:-}"
request_id="${CHARTLY_REQUEST_ID:-}"
events_path="${CHARTLY_EVENTS_PATH:-}"

do_seed="0"
seed_name="default"
seed_count="100"
yes="0"

base_url_set="0"
timeout_set="0"
tenant_set="0"
request_set="0"
seed_name_set="0"
seed_count_set="0"
events_path_set="0"

# Args
while [ "$#" -gt 0 ]; do
  case "$1" in
    --base-url) base_url="${2:-}"; base_url_set="1"; shift 2 ;;
    --timeout) timeout="${2:-}"; timeout_set="1"; shift 2 ;;
    --tenant-id) tenant_id="${2:-}"; tenant_set="1"; shift 2 ;;
    --request-id) request_id="${2:-}"; request_set="1"; shift 2 ;;
    --seed) do_seed="1"; shift ;;
    --seed-name) seed_name="${2:-}"; seed_name_set="1"; shift 2 ;;
    --seed-count) seed_count="${2:-}"; seed_count_set="1"; shift 2 ;;
    --events-path) events_path="${2:-}"; events_path_set="1"; shift 2 ;;
    --yes) yes="1"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown arg: $1 (use --help)" ;;
  esac
done

# Missing-value guards for flags that require values
if [ "$base_url_set" = "1" ] && [ -z "$base_url" ]; then
  die "--base-url requires a value"
fi
if [ "$timeout_set" = "1" ] && [ -z "$timeout" ]; then
  die "--timeout requires a value"
fi
if [ "$tenant_set" = "1" ] && [ -z "$tenant_id" ]; then
  die "--tenant-id requires a value"
fi
if [ "$request_set" = "1" ] && [ -z "$request_id" ]; then
  die "--request-id requires a value"
fi
if [ "$seed_name_set" = "1" ] && [ -z "$seed_name" ]; then
  die "--seed-name requires a value"
fi
if [ "$seed_count_set" = "1" ] && [ -z "$seed_count" ]; then
  die "--seed-count requires a value"
fi
if [ "$events_path_set" = "1" ] && [ -z "$events_path" ]; then
  die "--events-path requires a value"
fi

# Normalize base_url (strip trailing slashes)
while :; do
  case "$base_url" in
    */) base_url="${base_url%/}" ;;
    *) break ;;
  esac
done
[ -n "$base_url" ] || die "--base-url required"

# Validate integers (POSIX case)
case "$timeout" in
  ''|*[!0-9]*) die "--timeout must be integer seconds" ;;
esac
[ "$timeout" -ge 1 ] || die "--timeout must be >= 1"

case "$seed_count" in
  ''|*[!0-9]*) die "--seed-count must be integer" ;;
esac
[ "$seed_count" -ge 1 ] || die "--seed-count must be >= 1"

# Normalize events_path if set
if [ -n "$events_path" ]; then
  case "$events_path" in
    /*) : ;;
    *) events_path="/$events_path" ;;
  esac
fi

# Generate request_id if missing (no date/random allowed)
if [ -z "$request_id" ]; then
  request_id="it-$$"
fi

# Guard JSON-interpolated strings (no escaping tools available)
if [ -n "$tenant_id" ]; then
  case "$tenant_id" in
    *[!A-Za-z0-9._-]*) die "--tenant-id has unsupported chars (allowed: A-Z a-z 0-9 . _ -)" ;;
  esac
fi
case "$seed_name" in
  ''|*[!A-Za-z0-9._-]*) die "--seed-name has unsupported chars (allowed: A-Z a-z 0-9 . _ -)" ;;
esac

# Valid W3C traceparent (constant is fine for integration)
traceparent="00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"

tests="0"
failures="0"

is_2xx() {
  case "$1" in
    2??) return 0 ;;
    *) return 1 ;;
  esac
}

# http_request(method, path, body_or_empty)
# Sets: last_url, last_code, last_rc
last_url=""
last_code=""
last_rc="0"

http_request() {
  method="$1"
  path="$2"
  body="${3:-}"
  last_url="$base_url$path"
  last_code=""
  last_rc="0"

  # Keep suite running: capture curl exit code without set +e
  if [ -n "$body" ]; then
    if [ -n "$tenant_id" ]; then
      last_code="$(curl -sS --max-time "$timeout" -o /dev/null -w "%{http_code}" \
        -X "$method" \
        -H "accept: application/json" \
        -H "content-type: application/json" \
        -H "traceparent: $traceparent" \
        -H "x-request-id: $request_id" \
        -H "x-chartly-tenant: $tenant_id" \
        --data "$body" \
        "$last_url")" || last_rc="$?"
    else
      last_code="$(curl -sS --max-time "$timeout" -o /dev/null -w "%{http_code}" \
        -X "$method" \
        -H "accept: application/json" \
        -H "content-type: application/json" \
        -H "traceparent: $traceparent" \
        -H "x-request-id: $request_id" \
        --data "$body" \
        "$last_url")" || last_rc="$?"
    fi
  else
    if [ -n "$tenant_id" ]; then
      last_code="$(curl -sS --max-time "$timeout" -o /dev/null -w "%{http_code}" \
        -X "$method" \
        -H "accept: application/json" \
        -H "traceparent: $traceparent" \
        -H "x-request-id: $request_id" \
        -H "x-chartly-tenant: $tenant_id" \
        "$last_url")" || last_rc="$?"
    else
      last_code="$(curl -sS --max-time "$timeout" -o /dev/null -w "%{http_code}" \
        -X "$method" \
        -H "accept: application/json" \
        -H "traceparent: $traceparent" \
        -H "x-request-id: $request_id" \
        "$last_url")" || last_rc="$?"
    fi
  fi
}

run_check() {
  name="$1"
  method="$2"
  path="$3"
  body="${4:-}"

  tests=$((tests + 1))
  say "TEST: $name ($method $path)"

  last_rc="0"
  http_request "$method" "$path" "$body"

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
say "seed:       $do_seed"
say "events_path:${events_path:-<none>}"

# Required
run_check "health" "GET" "/health"
run_check "ready"  "GET" "/ready"

# Optional seed (mutating)
if [ "$do_seed" = "1" ]; then
  [ "$yes" = "1" ] || die "--seed requires --yes (mutating operation)"
  if [ -n "$tenant_id" ]; then
    seed_body='{"tenant_id":"'"$tenant_id"'","seed":"'"$seed_name"'","count":'"$seed_count"'}'
  else
    seed_body='{"seed":"'"$seed_name"'","count":'"$seed_count"'}'
  fi
  run_check "seed_data" "POST" "/v1/dev/seed" "$seed_body"
fi

# Optional events
if [ -n "$events_path" ]; then
  event_body='{"kind":"integration_test","message":"hello from integration_test.sh"}'
  run_check "events_post" "POST" "$events_path" "$event_body"
fi

passes=$((tests - failures))
say "SUMMARY: tests=$tests pass=$passes fail=$failures"

if [ "$failures" -ne 0 ]; then
  exit 1
fi
exit 0
