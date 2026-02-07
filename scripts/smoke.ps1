<[
.SYNOPSIS
  Basic HTTP smoke checks for /health and /ready.

.USAGE
  .\scripts\smoke.ps1 -BaseUrl http://localhost:8080
]>
param(
  [string]$BaseUrl = "http://localhost:8080",
  [string]$TenantId = "local",
  [string]$RequestId = "req_smoke"
)

Write-Host "== smoke.ps1 =="
Write-Host "base: $BaseUrl"

$headers = @{
  "X-Tenant-Id"  = $TenantId
  "X-Request-Id" = $RequestId
}

function Invoke-Check($path) {
  $url = $BaseUrl.TrimEnd("/") + $path
  Write-Host "GET $url"
  try {
    $resp = Invoke-WebRequest -Uri $url -Headers $headers -Method GET -TimeoutSec 10
    Write-Host "status: $($resp.StatusCode)"
    Write-Host $resp.Content
  } catch {
    Write-Host "error: $($_.Exception.Message)"
    throw
  }
}

Invoke-Check "/health"
Invoke-Check "/ready"
