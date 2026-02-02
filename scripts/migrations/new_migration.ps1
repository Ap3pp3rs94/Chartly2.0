<##>
# Create a new migration stub
# Usage: .\scripts\migrations\new_migration.ps1 -Name "create_table"
<##>

[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string]$Name
)

function Fail([string]$Message) { Write-Error $Message; exit 1 }

$migs = Join-Path $PSScriptRoot "migrations"
if (-not (Test-Path -LiteralPath $migs)) { New-Item -ItemType Directory -Force -Path $migs | Out-Null }

$name = $Name.Trim().ToLower() -replace '[^a-z0-9_]+','_'
if ([string]::IsNullOrWhiteSpace($name)) { Fail "Invalid migration name" }

$ts = (Get-Date).ToString("yyyyMMddHHmmss")
$up = Join-Path $migs ("{0}_{1}.sql" -f $ts, $name)
$down = Join-Path $migs ("{0}_{1}_down.sql" -f $ts, $name)

@"-- Migration: $ts_$name
-- Up
"@ | Set-Content -LiteralPath $up -Encoding utf8

@"-- Migration: $ts_$name
-- Down
"@ | Set-Content -LiteralPath $down -Encoding utf8

Write-Host "Created: $up"
Write-Host "Created: $down"
