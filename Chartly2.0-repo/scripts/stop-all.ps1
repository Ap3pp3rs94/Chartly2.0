#!/usr/bin/env pwsh
[CmdletBinding()]
param(
  # Drone compose projects created by recommended scripts use "chartly-drone-<id>".
  [string]$DroneProjectPrefix = "chartly-drone-",
  [switch]$SkipControlPlane,
  [switch]$SkipDrones,
  [switch]$Quiet
)

$ErrorActionPreference = "Continue"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $repoRoot

function Info([string]$m) { if (-not $Quiet) { Write-Host "[INFO] $m" -ForegroundColor Cyan } }
function Warn([string]$m) { if (-not $Quiet) { Write-Host "[WARN] $m" -ForegroundColor Yellow } }
function Err([string]$m)  { if (-not $Quiet) { Write-Host "[ERROR] $m" -ForegroundColor Red } }

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
  Err "docker not found on PATH"
  exit 1
}

$fail = 0

function RunDocker([string[]]$args, [string]$label) {
  & docker @args
  if ($LASTEXITCODE -ne 0) {
    Warn "$label (exit=$LASTEXITCODE)"
    $script:fail++
  }
}

if (-not $SkipControlPlane) {
  Info "Stopping control plane (docker-compose.control.yml)..."
  RunDocker @("compose","-f","docker-compose.control.yml","down","--remove-orphans") "control plane down failed"
}

if (-not $SkipDrones) {
  Info "Stopping drone projects (prefix='$DroneProjectPrefix')..."

  # Find running containers that match the prefix, then derive compose project names.
  # Container names are typically: <project>-drone-1 (service name is 'drone').
  $prefixEsc = [Regex]::Escape($DroneProjectPrefix)
  $names = & docker ps --filter "name=$DroneProjectPrefix" --format "{{.Names}}"

  $projects = New-Object System.Collections.Generic.HashSet[string]
  foreach ($n in $names) {
    $n = ($n ?? "").Trim()
    if ([string]::IsNullOrWhiteSpace($n)) { continue }

    if ($n -match "^($prefixEsc[a-z0-9\-]+)-drone-\d+$") {
      [void]$projects.Add($Matches[1])
      continue
    }
    if ($n -match "^($prefixEsc[a-z0-9\-]+)") {
      [void]$projects.Add($Matches[1])
      continue
    }
  }

  $projList = $projects.ToArray() | Sort-Object
  if (-not $projList -or $projList.Count -eq 0) {
    Info "No running drone containers matched."
  } else {
    foreach ($p in $projList) {
      Info "Stopping drone compose project: $p"
      RunDocker @("compose","-p",$p,"-f","docker-compose.drone.yml","down","--remove-orphans") "drone project '$p' down failed"
    }
  }

  # Best-effort: stop a single-drone default project (older workflows that didn't use -p).
  Info "Stopping default drone project (best-effort)..."
  RunDocker @("compose","-f","docker-compose.drone.yml","down","--remove-orphans") "default drone down failed"
}

if ($fail -eq 0) {
  Info "Stop complete."
  exit 0
}

Err "Stop complete with failures: $fail"
exit 1
