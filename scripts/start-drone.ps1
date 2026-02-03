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
