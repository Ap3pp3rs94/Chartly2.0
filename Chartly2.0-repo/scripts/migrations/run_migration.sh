#!/usr/bin/env bash
set -euo pipefail

# Chartly 2.0  run_migration.sh
# Applies exactly ONE migration SQL file via docker compose + psql, then records it.

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"

say() { printf '[run_migration] %s\n' "$*"; }
die() { printf '[run_migration] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage:
  ./run_migration.sh --yes --file <path/to/migration.sql>

Safety:
  - Requires --yes
  - Refuses to run in CI unless CHARTLY_ALLOW_MIGRATIONS_IN_CI=1

Env (defaults):
  CHARTLY_DB_SERVICE=postgres
  CHARTLY_DB_NAME=chartly
  CHARTLY_DB_USER=postgres

EOF
}

yes="0"
file=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes) yes="1"; shift ;;
    --file) file="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown arg: $1 (use --help)" ;;
  esac
done

[[ "$yes" == "1" ]] || { usage; die "Refusing to run without --yes"; }
[[ -n "$file" ]] || { usage; die "Missing required --file"; }

# CI safety gate
if [[ -n "${CI:-}" && "${CHARTLY_ALLOW_MIGRATIONS_IN_CI:-0}" != "1" ]]; then
  die "Refusing to run in CI. Set CHARTLY_ALLOW_MIGRATIONS_IN_CI=1 to override."
fi

# Resolve file (allow relative paths)
if [[ "$file" != /* ]]; then
  file="$(cd "$PWD" && pwd)/$file"
fi
[[ -f "$file" ]] || die "Migration file not found: $file"

db_service="${CHARTLY_DB_SERVICE:-postgres}"
db_name="${CHARTLY_DB_NAME:-chartly}"
db_user="${CHARTLY_DB_USER:-postgres}"

say "repo_root: $repo_root"
say "service:   $db_service"
say "db:        $db_name"
say "user:      $db_user"
say "file:      $file"

command -v docker >/dev/null 2>&1 || die "docker not found"

# Find compose file
compose_file=""
if [[ -f "$repo_root/docker-compose.yml" ]]; then
  compose_file="$repo_root/docker-compose.yml"
elif [[ -f "$repo_root/docker/compose.yml" ]]; then
  compose_file="$repo_root/docker/compose.yml"
fi
[[ -n "$compose_file" ]] || die "No docker compose file found (docker-compose.yml or docker/compose.yml)"
say "compose:   $compose_file"

# Ensure schema_migrations exists
say "Ensuring schema_migrations table exists..."
docker compose -f "$compose_file" exec -T "$db_service" \
  psql -v ON_ERROR_STOP=1 -U "$db_user" -d "$db_name" -c \
  "CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());" >/dev/null

version="$(basename "$file")"

# Guard: prevent re-applying the same version
already="$(docker compose -f "$compose_file" exec -T "$db_service" \
  psql -At -U "$db_user" -d "$db_name" -c \
  "SELECT 1 FROM schema_migrations WHERE version='${version}' LIMIT 1;" || true)"
already="$(echo "$already" | tr -d '\r\n')"

if [[ "$already" == "1" ]]; then
  die "Migration already applied: $version"
fi

say "Applying migration: $version"
sql="$(cat "$file")"
docker compose -f "$compose_file" exec -T "$db_service" \
  psql -v ON_ERROR_STOP=1 -U "$db_user" -d "$db_name" -c "$sql"

say "Recording migration: $version"
docker compose -f "$compose_file" exec -T "$db_service" \
  psql -v ON_ERROR_STOP=1 -U "$db_user" -d "$db_name" -c \
  "INSERT INTO schema_migrations(version) VALUES ('${version}');" >/dev/null

say "done"
