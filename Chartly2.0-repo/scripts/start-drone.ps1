#!/usr/bin/env pwsh
param(
    [Parameter(Mandatory=$true)][string]$ControlPlane,
    [string]$DroneId = [guid]::NewGuid().ToString()
)
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

Write-Host "Starting drone $DroneId..." -ForegroundColor Cyan
Write-Host "Control plane: $ControlPlane" -ForegroundColor White

$env:CONTROL_PLANE = $ControlPlane
$env:DRONE_ID = $DroneId

docker compose -f docker-compose.drone.yml build
docker compose -f docker-compose.drone.yml up -d

Write-Host "`n Drone started!" -ForegroundColor Green
Write-Host "  Drone ID: $DroneId" -ForegroundColor White
Write-Host "  Logs: docker compose -f docker-compose.drone.yml logs -f" -ForegroundColor White

Write-Host "`n Quick checks:" -ForegroundColor Yellow
try {
    Invoke-RestMethod http://localhost:8090/api/drones | Out-Host
} catch {
    Write-Host "  Failed to query /api/drones" -ForegroundColor Red
}
try {
    Invoke-RestMethod http://localhost:8090/api/results/summary | Out-Host
} catch {
    Write-Host "  Failed to query /api/results/summary" -ForegroundColor Red
}
