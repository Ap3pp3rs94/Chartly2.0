<[
.SYNOPSIS
  Start local Chartly docker compose stack.

.USAGE
  .\scripts\up.ps1 -ComposeFile docker-compose.yml
]>
param(
  [string]$ComposeFile = "docker-compose.yml"
)

Write-Host "== up.ps1 =="
if (-not (Test-Path -LiteralPath $ComposeFile)) {
  throw "Compose file not found: $ComposeFile"
}

Write-Host "compose: $ComposeFile"

docker compose -f $ComposeFile up -d
