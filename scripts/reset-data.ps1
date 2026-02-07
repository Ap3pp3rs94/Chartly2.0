#!/usr/bin/env pwsh
[CmdletBinding()]
param(
  # control-plane: wipes data/control-plane/*
  # all: wipes data/*
  [ValidateSet("control-plane","all")]
  [string]$Scope = "control-plane",

  # Destructive action gate
  [switch]$Apply,

  # Skip interactive confirmation (use carefully)
  [switch]$Force,

  # Stop services before deleting (recommended)
  [switch]$StopFirst,

  [switch]$Quiet
)

$ErrorActionPreference = "Stop"
$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $repoRoot

function Info([string]$m) { if (-not $Quiet) { Write-Host "[INFO] $m" -ForegroundColor Cyan } }
function Warn([string]$m) { if (-not $Quiet) { Write-Host "[WARN] $m" -ForegroundColor Yellow } }
function Err([string]$m)  { if (-not $Quiet) { Write-Host "[ERROR] $m" -ForegroundColor Red } }

$dataRoot = Join-Path $repoRoot "data"
$targetRoot = if ($Scope -eq "all") { $dataRoot } else { Join-Path $dataRoot "control-plane" }

Warn "RESET DATA requested (scope=$Scope)"
Warn "Target root: $targetRoot"

if (-not (Test-Path $targetRoot)) {
  Info "Nothing to reset (missing: $targetRoot)"
  exit 0
}

# Only delete children (keep the root directory itself).
$items = @(Get-ChildItem -LiteralPath $targetRoot -Force -ErrorAction SilentlyContinue)
$cnt = $items.Count
Info "Items to delete: $cnt"

if (-not $Apply) {
  Info "Dry-run only. Re-run with -Apply to perform deletion."
  if ($cnt -gt 0 -and -not $Quiet) {
    Write-Host "Preview (first 20):" -ForegroundColor DarkGray
    $items | Select-Object -First 20 | ForEach-Object { Write-Host "  $($_.FullName)" -ForegroundColor DarkGray }
    if ($cnt -gt 20) { Write-Host "  ... ($cnt total)" -ForegroundColor DarkGray }
  }
  exit 0
}

if (-not $Force) {
  $confirm = Read-Host "Type RESET to confirm destructive delete under '$targetRoot'"
  if ($confirm -ne "RESET") {
    Warn "Cancelled (confirmation did not match)."
    exit 0
  }
}

if ($StopFirst) {
  $stop = Join-Path $PSScriptRoot "stop-all.ps1"
  if (Test-Path $stop) {
    Info "Stopping services first..."
    & $stop -Quiet:$Quiet | Out-Host
  } else {
    Warn "stop-all.ps1 not found; continuing without stopping."
  }
}

# Safety: ensure targetRoot resolves under repoRoot.
$resolvedTarget = (Resolve-Path $targetRoot).Path
$resolvedRepo   = $repoRoot.Path
if (-not $resolvedTarget.StartsWith($resolvedRepo, [System.StringComparison]::OrdinalIgnoreCase)) {
  throw "Refusing to delete outside repo root."
}
if ($resolvedTarget -eq $resolvedRepo) {
  throw "Refusing to delete repo root."
}

foreach ($it in $items) {
  Remove-Item -LiteralPath $it.FullName -Recurse -Force -ErrorAction SilentlyContinue
}

Info "Data reset complete."

# Re-bootstrap required dirs (best-effort)
$bootstrap = Join-Path $PSScriptRoot "control-plane-bootstrap.ps1"
if (Test-Path $bootstrap) {
  Info "Re-running bootstrap..."
  & $bootstrap | Out-Host
} else {
  Warn "Bootstrap script not found: $bootstrap"
}

exit 0
