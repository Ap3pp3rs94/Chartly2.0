#!/usr/bin/env pwsh
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

Write-Host "Building control plane..." -ForegroundColor Cyan
docker compose -f docker-compose.control.yml build

Write-Host "Starting control plane..." -ForegroundColor Cyan
docker compose -f docker-compose.control.yml up -d

Write-Host "Waiting for health on http://localhost:8090/health ..." -ForegroundColor Yellow
$timeout = 60
$elapsed = 0
while ($elapsed -lt $timeout) {
    try {
        $health = Invoke-RestMethod -Uri http://localhost:8090/health -TimeoutSec 5
        if ($health.status -eq "healthy" -or $health.status -eq "degraded") {
            Write-Host "`n Control plane responding!" -ForegroundColor Green
            Write-Host "  Gateway:   http://localhost:8090" -ForegroundColor White
            Write-Host "  Health:    http://localhost:8090/health" -ForegroundColor White
            Write-Host "  Status:    http://localhost:8090/api/status" -ForegroundColor White
            Write-Host "  Profiles:  http://localhost:8090/api/profiles" -ForegroundColor White
            Write-Host "  Results:   http://localhost:8090/api/results" -ForegroundColor White
            Write-Host "  Drones:    http://localhost:8090/api/drones" -ForegroundColor White
            Write-Host "  Runs:      http://localhost:8090/api/runs" -ForegroundColor White
            Write-Host "  Records:   http://localhost:8090/api/records" -ForegroundColor White
            Write-Host "  Reports:   http://localhost:8090/api/reports" -ForegroundColor White
            exit 0
        }
    } catch { }
    Start-Sleep -Seconds 2
    $elapsed += 2
    Write-Host "." -NoNewline
}
Write-Host "`n Timeout waiting for control plane" -ForegroundColor Red
docker compose -f docker-compose.control.yml logs
exit 1
