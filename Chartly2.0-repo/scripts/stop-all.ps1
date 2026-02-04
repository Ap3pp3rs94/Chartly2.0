#!/usr/bin/env pwsh
$ErrorActionPreference = "SilentlyContinue"
Set-Location (Join-Path $PSScriptRoot "..")

Write-Host "Stopping all services..." -ForegroundColor Yellow
docker compose -f docker-compose.control.yml down 2>$null | Out-Null
docker compose -f docker-compose.drone.yml down 2>$null | Out-Null
Write-Host " All stopped" -ForegroundColor Green
