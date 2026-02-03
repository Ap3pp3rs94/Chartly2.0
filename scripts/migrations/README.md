# scripts/migrations

A tiny Postgres migration system for Chartly 2.0.

## Philosophy

- Works today with docker compose + postgres.
- No external migration tool required (yet).
- Stores a single table: `schema_migrations(version text primary key, applied_at timestamptz not null default now())`
- Migrations are plain SQL files in `scripts/migrations/migrations/` named:
  `YYYYMMDDHHMMSS_description.sql`

## Requirements

- docker + docker compose
- postgres service in compose (default: `postgres`)
- psql available inside the postgres container (official images have it)

## Env / args

- `CHARTLY_DB_SERVICE`  docker compose service name (default: postgres)
- `CHARTLY_DB_NAME`  database (default: chartly)
- `CHARTLY_DB_USER`  user (default: postgres)

## Usage

```powershell
# apply pending migrations
.\scripts\migrations\migrate.ps1

# show status
.\scripts\migrations\status.ps1

# create new migration template
.\scripts\migrations\new_migration.ps1 -Name "create_events_table"

# rollback last migration (if possible)
.\scripts\migrations\rollback.ps1 -Yes
```
