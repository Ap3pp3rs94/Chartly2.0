#!/usr/bin/env bash
set -euo pipefail

# Chartly 2.0  reset_db.sh
# DANGEROUS: Resets local dev database container(s). Requires explicit opt-in.

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"

say() { printf '[reset_db] %s\n' "$*"; }
die() { printf '[reset_db] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage:
  ./reset_db.sh --yes [--service <name>]

Safety:
  - Requires --yes
  - Will NOT run in CI unless CHARTLY_ALLOW_DB_RESET=1

Env:
  CHARTLY_DB_SERVICE        Optional service override (e.g. postgres)
  CHARTLY_ALLOW_DB_RESET=1  Allow in CI environments

EOF
}

yes="0"
svc="${CHARTLY_DB_SERVICE:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes) yes="1"; shift ;;
    --service) svc="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown arg: $1 (use --help)" ;;
  esac
done

if [[ "$yes" != "1" ]]; then
  usage
  die "Refusing to run without --yes"
fi

# CI safety gate
if [[ -n "${CI:-}" && "${CHARTLY_ALLOW_DB_RESET:-0}" != "1" ]]; then
  die "Refusing to run in CI. Set CHARTLY_ALLOW_DB_RESET=1 to override."
fi

# Find compose file
compose_file=""
if [[ -f "$repo_root/docker-compose.yml" ]]; then
  compose_file="$repo_root/docker-compose.yml"
elif [[ -f "$repo_root/docker/compose.yml" ]]; then
  compose_file="$repo_root/docker/compose.yml"
fi

[[ -n "$compose_file" ]] || die "No docker compose file found (docker-compose.yml or docker/compose.yml)"
command -v docker >/dev/null 2>&1 || die "docker not found"

say "repo_root: $repo_root"
say "compose:   $compose_file"

# If service not specified, try to infer from compose config
if [[ -z "$svc" ]]; then
  say "Inferring DB service name from compose..."
  # Try common service names first
  for candidate in postgres postgresql db database; do
    if docker compose -f "$compose_file" config --services | grep -qx "$candidate"; then
      svc="$candidate"
      break
    fi
  done
fi

[[ -n "$svc" ]] || die "Could not infer DB service. Provide --service <name> or set CHARTLY_DB_SERVICE."

say "DB service: $svc"
say "Stopping/removing container: $svc"
docker compose -f "$compose_file" rm -sf "$svc"

say "Starting service: $svc"
docker compose -f "$compose_file" up -d "$svc"

say "Status:"
docker compose -f "$compose_file" ps "$svc" || true

say "done"
