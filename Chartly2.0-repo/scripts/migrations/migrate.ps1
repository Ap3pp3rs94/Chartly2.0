<##>
# Apply pending migrations (SAFE)
# Usage: .\scripts\migrations\migrate.ps1
<##>

[CmdletBinding()]
param()

function Say([string]$Message) { Write-Host ("[migrate] " + $Message) }
function Fail([string]$Message) { Write-Error $Message; exit 1 }

$migs = Join-Path $PSScriptRoot "migrations"

$service = $env:CHARTLY_DB_SERVICE
if ([string]::IsNullOrWhiteSpace($service)) { $service = "postgres" }
$dbName = $env:CHARTLY_DB_NAME
if ([string]::IsNullOrWhiteSpace($dbName)) { $dbName = "chartly" }
$dbUser = $env:CHARTLY_DB_USER
if ([string]::IsNullOrWhiteSpace($dbUser)) { $dbUser = "postgres" }

if (-not (Test-Path -LiteralPath $migs)) { Fail "Migrations folder missing: $migs" }
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { Fail "docker not found" }

Say "service=$service db=$dbName user=$dbUser"

$initSql = "CREATE TABLE IF NOT EXISTS schema_migrations (`n  version text PRIMARY KEY,`n  applied_at timestamptz NOT NULL DEFAULT now()`n);`n"

$initSql | docker compose exec -T $service psql -U $dbUser -d $dbName 1>$null 2>$null
if ($LASTEXITCODE -ne 0) { Fail "Failed to initialize schema_migrations table" }

$applied = docker compose exec -T $service psql -U $dbUser -d $dbName -t -A -c "SELECT version FROM schema_migrations ORDER BY version;"
if ($LASTEXITCODE -ne 0) { Fail "Failed to read schema_migrations" }

$appliedSet = @{}
$applied.Split([Environment]::NewLine) | ForEach-Object { if (-not [string]::IsNullOrWhiteSpace($_)) { $appliedSet[$_] = $true } }

$files = Get-ChildItem -LiteralPath $migs -Filter "*.sql" | Sort-Object Name
if ($files.Count -eq 0) { Say "No migrations found."; exit 0 }

$pending = @()
foreach ($f in $files) {
  if (-not $appliedSet.ContainsKey($f.BaseName)) { $pending += $f }
}

if ($pending.Count -eq 0) { Say "No pending migrations."; exit 0 }

foreach ($f in $pending) {
  Say "Applying $($f.Name)"
  docker compose exec -T $service psql -U $dbUser -d $dbName -f "/scripts/migrations/migrations/$($f.Name)"
  if ($LASTEXITCODE -ne 0) { Fail "Migration failed: $($f.Name)" }
  docker compose exec -T $service psql -U $dbUser -d $dbName -c "INSERT INTO schema_migrations(version) VALUES('$($f.BaseName)');" | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail "Failed to record migration: $($f.BaseName)" }
}

Say "done"
