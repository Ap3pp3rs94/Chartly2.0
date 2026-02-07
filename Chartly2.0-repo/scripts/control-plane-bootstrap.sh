#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

printf "%s\n" "Bootstrapping control-plane workspace..." 

mkdir -p "$ROOT/data/control-plane"
mkdir -p "$ROOT/profiles/government"

count="$(ls -1 "$ROOT/profiles/government"/*.yaml 2>/dev/null | wc -l | tr -d ' ')"
if [[ "$count" == "0" ]]; then
  printf "%s\n" "WARNING: No profiles found in profiles/government/*.yaml"
else
  printf "%s\n" "Profiles found: $count"
fi

if [[ ! -f "$ROOT/web/dist/index.html" ]]; then
  printf "%s\n" "NOTE: web/dist not found. APIs will work; UI will be missing until web is built."
fi

printf "%s\n" "Bootstrap complete."