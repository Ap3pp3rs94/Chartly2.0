#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail=0
warn=0

say() { printf "%s\n" "$*"; }
pass() { say " $*"; }
w() { warn=$((warn+1)); say " $*"; }
f() { fail=$((fail+1)); say " $*"; }

cd "$ROOT"
say "Chartly Control-Plane Doctor (bash)"
say "Repo root: $ROOT"
say ""

command -v docker >/dev/null 2>&1 && pass "docker CLI found" || { f "docker CLI not found"; }

if docker compose version >/dev/null 2>&1; then
  pass "docker compose available"
else
  f "docker compose not available"
fi

if docker info >/dev/null 2>&1; then
  pass "Docker engine reachable"
else
  f "Docker engine not reachable (is Docker running?)"
fi

req=(
  "docker-compose.control.yml"
  "docker-compose.drone.yml"
  "cmd/drone/Dockerfile"
  "profiles/government"
  "scripts"
)
for p in "${req[@]}"; do
  if [[ -e "$ROOT/$p" ]]; then pass "Exists: $p"; else f "Missing: $p"; fi
done

profiles_dir="$ROOT/profiles/government"
if [[ -d "$profiles_dir" ]]; then
  count="$(ls -1 "$profiles_dir"/*.yaml 2>/dev/null | wc -l | tr -d ' ')"
  if [[ "$count" == "0" ]]; then w "No profiles found in profiles/government/*.yaml"; else pass "Profiles present: $count file(s)"; fi
fi

mkdir -p "$ROOT/data/control-plane" 2>/dev/null || true
probe="$ROOT/data/control-plane/.write_probe"
if printf "ok" > "$probe" 2>/dev/null; then
  rm -f "$probe" || true
  pass "Writable: data/control-plane"
else
  f "data/control-plane not writable"
fi

check_port() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    if lsof -iTCP:"$port" -sTCP:LISTEN -P -n >/dev/null 2>&1; then
      f "Port $port is in use"
    else
      pass "Port $port is free"
    fi
  elif command -v ss >/dev/null 2>&1; then
    if ss -ltn 2>/dev/null | grep -qE "[:.]$port\b"; then
      f "Port $port is in use"
    else
      pass "Port $port is free"
    fi
  else
    w "Port check skipped for $port (need lsof or ss)"
  fi
}

# Default expected ports from compose examples. Adjust if you changed docker-compose.control.yml.
for p in 80 8081 8082 8083; do
  check_port "$p"
done

if [[ -f "$ROOT/web/dist/index.html" ]]; then
  pass "UI present: web/dist/index.html"
else
  w "UI not built (web/dist missing); APIs still work"
fi

say ""
if [[ "$fail" -eq 0 ]]; then
  say "OK code=0"
  exit 0
fi
say "FAILED code=1 (fails=$fail warns=$warn)"
exit 1