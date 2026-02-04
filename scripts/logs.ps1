#!/usr/bin/env pwsh
[CmdletBinding()]
param(
  # Control-plane service name (registry|aggregator|coordinator|gateway|all)
  [string]$Service = "all",

  # Tail lines
  [int]$Tail = 100,

  # If set, tails logs for that drone compose project instead of control-plane
  [string]$DroneProject = "",

  # Disable follow mode
  [switch]$NoFollow
)

$ErrorActionPreference = "Stop"
$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $repoRoot

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
  Write-Host "[ERROR] docker not found on PATH" -ForegroundColor Red
  exit 1
}

$followArgs = @()
if (-not $NoFollow) { $followArgs += "-f" }

if (-not [string]::IsNullOrWhiteSpace($DroneProject)) {
  Write-Host "[INFO] Tailing drone logs (project=$DroneProject)..." -ForegroundColor Cyan
  $args = @("compose","-p",$DroneProject,"-f","docker-compose.drone.yml","logs") + $followArgs + @("--tail",$Tail)
  & docker @args
  exit $LASTEXITCODE
}

Write-Host "[INFO] Tailing control-plane logs..." -ForegroundColor Cyan
if ($Service -eq "all") {
  $args = @("compose","-f","docker-compose.control.yml","logs") + $followArgs + @("--tail",$Tail)
} else {
  $args = @("compose","-f","docker-compose.control.yml","logs") + $followArgs + @("--tail",$Tail,$Service)
}
& docker @args
exit $LASTEXITCODE
