#!/usr/bin/env pwsh
[CmdletBinding()]
param(
  [string]$EnvFile = ".env.local"
)

$ErrorActionPreference = "Stop"
$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $repoRoot

function Fail([string]$msg) {
  Write-Host "[ERROR] $msg" -ForegroundColor Red
  exit 1
}
function Info([string]$msg) {
  Write-Host "[INFO] $msg" -ForegroundColor Cyan
}
function Warn([string]$msg) {
  Write-Host "[WARN] $msg" -ForegroundColor Yellow
}

$path = Join-Path $repoRoot $EnvFile
if (-not (Test-Path $path)) {
  Fail "Missing $EnvFile. Copy .env.local.template -> .env.local, then paste your key."
}

Info "Loading environment from: $EnvFile"
$lines = Get-Content -LiteralPath $path -ErrorAction Stop

foreach ($line in $lines) {
  $t = ($line ?? "").Trim()
  if ($t.Length -eq 0) { continue }
  if ($t.StartsWith("#")) { continue }

  $parts = $t.Split("=", 2)
  if ($parts.Count -ne 2) { continue }

  $k = $parts[0].Trim()
  $v = $parts[1].Trim()

  if (($v.StartsWith('"') -and $v.EndsWith('"')) -or ($v.StartsWith("'") -and $v.EndsWith("'"))) {
    $v = $v.Substring(1, $v.Length - 2)
  }

  if ([string]::IsNullOrWhiteSpace($k)) { continue }

  $env:$k = $v
}

$required = @("DATA_GOV_API_KEY")
foreach ($k in $required) {
  $v = (Get-Item "Env:$k" -ErrorAction SilentlyContinue).Value
  if ([string]::IsNullOrWhiteSpace($v)) {
    Fail "Required env var missing: $k (set it in .env.local)"
  }
  if ($v -like "*__PASTE_DATA_GOV_API_KEY_HERE__*") {
    Fail "DATA_GOV_API_KEY is still the placeholder. Paste the real key into .env.local."
  }
  if ($v.Length -lt 10) {
    Fail "DATA_GOV_API_KEY looks too short. Paste the full key into .env.local."
  }
}

Info "Loaded env vars:"
Info "  DATA_GOV_API_KEY = [SET] (hidden)"
$ua = (Get-Item "Env:CHARTLY_USER_AGENT" -ErrorAction SilentlyContinue).Value
if ($ua) { Info "  CHARTLY_USER_AGENT = [SET]" } else { Warn "  CHARTLY_USER_AGENT = [NOT SET] (optional)" }

$cp = (Get-Item "Env:CONTROL_PLANE" -ErrorAction SilentlyContinue).Value
if ($cp) { Info "  CONTROL_PLANE = $cp" }

$pi = (Get-Item "Env:PROCESS_INTERVAL" -ErrorAction SilentlyContinue).Value
if ($pi) { Info "  PROCESS_INTERVAL = $pi" }

Info "OK. You can now start the drone in this same terminal session."
exit 0
