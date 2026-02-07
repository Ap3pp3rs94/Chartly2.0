<[
.SYNOPSIS
  Dev loop smoke test for /health and /ready.

.USAGE
  .\scripts\dev\smoke.ps1
]>
param(
  [string]$BaseUrl = $env:CHARTLY_BASE_URL
)

if (-not $BaseUrl -or $BaseUrl.Trim() -eq "") {
  $BaseUrl = "http://localhost:8080"
}

$tenant = $env:CHARTLY_TENANT_ID
if (-not $tenant -or $tenant.Trim() -eq "") {
  $tenant = "local"
}

$requestId = $env:CHARTLY_REQUEST_ID
if (-not $requestId -or $requestId.Trim() -eq "") {
  $requestId = "req_dev_smoke"
}

$smokeScript = "C:\Chartly2.0\scripts\smoke.ps1"
if (-not (Test-Path -LiteralPath $smokeScript)) {
  throw "missing: $smokeScript"
}

& $smokeScript -BaseUrl $BaseUrl -TenantId $tenant -RequestId $requestId
