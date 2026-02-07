#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

SCOPE="control-plane"
APPLY="false"
FORCE="false"
STOP_FIRST="false"

usage() {
  cat <<EOF
Usage:
  ./scripts/reset-data.sh [--scope control-plane|all] [--apply] [--force] [--stop-first]

Defaults:
  --scope control-plane
  (dry-run unless --apply)

Safety:
  - Without --force, requires typing RESET interactively.
EOF
}

realpath_py() {
  python3 - <<PY "$1"
import os,sys
print(os.path.realpath(sys.argv[1]))
PY
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --scope) SCOPE="$2"; shift 2;;
    --apply) APPLY="true"; shift 1;;
    --force) FORCE="true"; shift 1;;
    --stop-first) STOP_FIRST="true"; shift 1;;
    -h|--help) usage; exit 0;;
    *) echo "Unknown arg: $1" >&2; usage; exit 2;;
  esac
done

if [[ "$SCOPE" != "control-plane" && "$SCOPE" != "all" ]]; then
  echo "Invalid --scope: $SCOPE" >&2
  exit 2
fi

DATA_ROOT="$ROOT/data"
TARGET="$DATA_ROOT/control-plane"
if [[ "$SCOPE" == "all" ]]; then TARGET="$DATA_ROOT"; fi

echo "[WARN] RESET DATA requested (scope=$SCOPE)"
echo "[WARN] Target root: $TARGET"

if [[ ! -e "$TARGET" ]]; then
  echo "[INFO] Nothing to reset (missing: $TARGET)"
  exit 0
fi

# Safety: ensure target resolves under repo root.
if command -v python3 >/dev/null 2>&1; then
  rt="$(realpath_py "$TARGET")"
  rr="$(realpath_py "$ROOT")"
  if [[ "$rt" != "$rr"* ]]; then
    echo "Refusing to delete outside repo root." >&2
    exit 1
  fi
fi

if [[ "$APPLY" != "true" ]]; then
  echo "[INFO] Dry-run only. Re-run with --apply to perform deletion."
  echo "[INFO] Would delete:"
  ls -la "$TARGET" | sed 's/^/  /' || true
  exit 0
fi

if [[ "$FORCE" != "true" ]]; then
  read -r -p "Type RESET to confirm destructive delete under '$TARGET': " ans
  if [[ "$ans" != "RESET" ]]; then
    echo "[WARN] Cancelled."
    exit 0
  fi
fi

if [[ "$STOP_FIRST" == "true" ]]; then
  echo "[INFO] Stopping services first..."
  "$ROOT/scripts/stop-all.sh" || true
fi

# Delete children only (keep directory)
shopt -s dotglob nullglob
rm -rf "$TARGET"/*

echo "[INFO] Data reset complete."
if [[ -x "$ROOT/scripts/control-plane-bootstrap.sh" ]]; then
  echo "[INFO] Re-running bootstrap..."
  "$ROOT/scripts/control-plane-bootstrap.sh"
else
  echo "[WARN] Bootstrap script not executable. You may need: chmod +x scripts/*.sh"
fi