#!/bin/sh
set -eu

# Chartly 2.0  load_test.sh
# Portable load ping using POSIX sh + curl + xargs concurrency.

say() { printf '[load_test] %s\n' "$*"; }
die() { printf '[load_test] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage:
  ./load_test.sh [--base-url <url>] [--endpoint <path>] [--n <requests>] [--c <concurrency>] [--timeout <sec>]

Defaults:
  --base-url   $CHARTLY_BASE_URL or http://localhost:8080
  --endpoint   /health
  --n          100
  --c          10
  --timeout    10

Requires:
  sh, curl, xargs

EOF
}

base_url="${CHARTLY_BASE_URL:-http://localhost:8080}"
endpoint="/health"
n="100"
c="10"
timeout="10"

# arg parsing (POSIX)
while [ "$#" -gt 0 ]; do
  case "$1" in
    --base-url) base_url="${2:-}"; shift 2 ;;
    --endpoint) endpoint="${2:-}"; shift 2 ;;
    --n) n="${2:-}"; shift 2 ;;
    --c) c="${2:-}"; shift 2 ;;
    --timeout) timeout="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown arg: $1 (use --help)" ;;
  esac
done

command -v curl >/dev/null 2>&1 || die "curl not found"
command -v xargs >/dev/null 2>&1 || die "xargs not found"

# normalize base_url (strip trailing /)
case "$base_url" in
  */) base_url="${base_url%/}" ;;
esac
[ -n "$base_url" ] || die "--base-url required"

# normalize endpoint (ensure leading /)
case "$endpoint" in
  /*) : ;;
  *) endpoint="/$endpoint" ;;
esac

# validate integers (POSIX, no grep)
case "$n" in
  ''|*[!0-9]*) die "--n must be integer" ;;
esac
case "$c" in
  ''|*[!0-9]*) die "--c must be integer" ;;
esac
case "$timeout" in
  ''|*[!0-9]*) die "--timeout must be integer" ;;
esac

# enforce non-zero where sensible
[ "$c" -ge 1 ] || die "--c must be >= 1"
[ "$timeout" -ge 1 ] || die "--timeout must be >= 1"

url="$base_url$endpoint"

say "url:         $url"
say "requests:    $n"
say "concurrency: $c"
say "timeout:     ${timeout}s"

tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t chartly_load_test)"
# shellcheck disable=SC2064
trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM

ok_file="$tmpdir/ok"
fail_file="$tmpdir/fail"
: > "$ok_file"
: > "$fail_file"

start="$(date +%s)"

# Worker command (POSIX): xargs spawns sh -c '...'
# We append 1 line per result into files; simple + portable.
worker='
u="$1"
to="$2"
ok="$3"
fail="$4"
if curl -fsS --max-time "$to" "$u" >/dev/null 2>&1; then
  echo 1 >> "$ok"
else
  echo 1 >> "$fail"
fi
'

# Generate 1..n without seq (POSIX loop)
i=1
while [ "$i" -le "$n" ]; do
  printf '%s\n' "$i"
  i=$((i + 1))
done | xargs -n 1 -P "$c" sh -c "$worker" _ "$url" "$timeout" "$ok_file" "$fail_file"

end="$(date +%s)"
elapsed=$((end - start))
[ "$elapsed" -gt 0 ] || elapsed=1

ok="$(wc -l < "$ok_file" | tr -d ' ')"
fail="$(wc -l < "$fail_file" | tr -d ' ')"

rps=$((n / elapsed))

say "elapsed_sec: $elapsed"
say "successes:   $ok"
say "failures:    $fail"
say "rps_approx:  $rps"
say "done"
