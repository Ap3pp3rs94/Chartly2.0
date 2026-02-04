<##>
# Show migration status
# Usage: .\scripts\migrations\status.ps1
<##>

[CmdletBinding()]
param()

function Say([string]$Message) { Write-Host ("[status] " + $Message) }
function Fail([string]$Message) { Write-Error $Message; exit 1 }

$migs = Join-Path $PSScriptRoot "migrations"

$service = $env:CHARTLY_DB_SERVICE
if ([string]::IsNullOrWhiteSpace($service)) { $service = "postgres" }
$dbName = $env:CHARTLY_DB_NAME
if ([string]::IsNullOrWhiteSpace($dbName)) { $dbName = "chartly" }
$dbUser = $env:CHARTLY_DB_USER
if ([string]::IsNullOrWhiteSpace($dbUser)) { $dbUser = "postgres" }

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

Say "Applied:"
$appliedSet.Keys | Sort-Object | ForEach-Object { Write-Host "  $_" }

Say "Pending:"
foreach ($f in $files) {
  if (-not $appliedSet.ContainsKey($f.BaseName) -and -not $f.BaseName.EndsWith('_down')) {
    Write-Host "  $($f.BaseName)"
  }
}
