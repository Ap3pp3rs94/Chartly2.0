<##>
# Roll back last migration (SAFE)
# Usage: .\scripts\migrations\rollback.ps1 -Yes
<##>

[CmdletBinding()]
param(
  [switch]$Yes
)

function Say([string]$Message) { Write-Host ("[rollback] " + $Message) }
function Fail([string]$Message) { Write-Error $Message; exit 1 }

if (-not $Yes) { Fail "Refusing to rollback without -Yes" }

$migs = Join-Path $PSScriptRoot "migrations"

$service = $env:CHARTLY_DB_SERVICE
if ([string]::IsNullOrWhiteSpace($service)) { $service = "postgres" }
$dbName = $env:CHARTLY_DB_NAME
if ([string]::IsNullOrWhiteSpace($dbName)) { $dbName = "chartly" }
$dbUser = $env:CHARTLY_DB_USER
if ([string]::IsNullOrWhiteSpace($dbUser)) { $dbUser = "postgres" }

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { Fail "docker not found" }

$last = docker compose exec -T $service psql -U $dbUser -d $dbName -t -A -c "SELECT version FROM schema_migrations ORDER BY applied_at DESC, version DESC LIMIT 1;"
if ($LASTEXITCODE -ne 0) { Fail "Failed to read schema_migrations" }
$last = $last.Trim()
if ([string]::IsNullOrWhiteSpace($last)) { Say "No migrations to rollback"; exit 0 }

Say "Last migration: $last"

$downFile = Join-Path $migs ("{0}_down.sql" -f $last)
if (-not (Test-Path -LiteralPath $downFile)) { Fail "Down migration not found: $downFile" }

Say "Applying down: $(Split-Path -Leaf $downFile)"
$downName = Split-Path -Leaf $downFile

docker compose exec -T $service psql -U $dbUser -d $dbName -f "/scripts/migrations/migrations/$downName"
if ($LASTEXITCODE -ne 0) { Fail "Rollback failed: $downName" }

$del = "DELETE FROM schema_migrations WHERE version='$last';"
docker compose exec -T $service psql -U $dbUser -d $dbName -c $del | Out-Null
if ($LASTEXITCODE -ne 0) { Fail "Failed to delete migration record: $last" }

Say "done"
