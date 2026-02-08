Param(
  [string]$BaseUrl = "http://localhost:8090",
  [string]$ComposeFile = "docker-compose.control.yml",
  [switch]$Logs
)

function Pass($msg) { Write-Host "PASS: $msg" -ForegroundColor Green }
function Fail($msg) { Write-Host "FAIL: $msg" -ForegroundColor Red; $script:Failed = $true }

$script:Failed = $false

Write-Host "== VERIFY ==" -ForegroundColor Cyan

Write-Host "-- /api/events (SSE) --"
try {
  $out = & curl.exe -sN --max-time 5 "$BaseUrl/api/events"
  if ($out -match "event:" -or $out -match "data:") { Pass "/api/events stream" } else { Fail "/api/events stream" }
} catch {
  Fail "/api/events stream"
}

Write-Host "-- /api/crypto/symbols --"
try {
  $resp = Invoke-WebRequest -Uri "$BaseUrl/api/crypto/symbols" -UseBasicParsing -Method Get
  if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 300) { Pass "/api/crypto/symbols ($($resp.StatusCode))" } else { Fail "/api/crypto/symbols ($($resp.StatusCode))" }
} catch {
  Fail "/api/crypto/symbols"
}

Write-Host "-- /api/crypto/top --"
try {
  $resp = Invoke-WebRequest -Uri "$BaseUrl/api/crypto/top?limit=5&direction=gainers&suffix=USDT" -UseBasicParsing -Method Get
  if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 300) { Pass "/api/crypto/top ($($resp.StatusCode))" } else { Fail "/api/crypto/top ($($resp.StatusCode))" }
} catch {
  Fail "/api/crypto/top"
}

Write-Host "-- /api/crypto/stream (SSE) --"
try {
  $out = & curl.exe -sN --max-time 5 "$BaseUrl/api/crypto/stream?limit=3&direction=gainers&suffix=USDT"
  if ($out -match "event:" -or $out -match "data:") { Pass "/api/crypto/stream stream" } else { Fail "/api/crypto/stream stream" }
} catch {
  Fail "/api/crypto/stream stream"
}

Write-Host "-- /api/reports (GET) --"
try {
  $resp = Invoke-WebRequest -Uri "$BaseUrl/api/reports" -UseBasicParsing -Method Get
  if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 300) { Pass "/api/reports ($($resp.StatusCode))" } else { Fail "/api/reports ($($resp.StatusCode))" }
} catch {
  Fail "/api/reports"
}

Write-Host "-- /api/reports (POST->GET) --"
try {
  $body = @{ profiles = @("crypto-watchlist"); mode = "auto" } | ConvertTo-Json -Depth 3
  $obj = Invoke-RestMethod -Uri "$BaseUrl/api/reports" -Method Post -ContentType "application/json" -Body $body
  $rid = $obj.id
  if (-not $rid) { $rid = $obj.report_id }
  if ($rid) {
    Pass "report create ($rid)"
    $resp = Invoke-WebRequest -Uri "$BaseUrl/api/reports/$rid" -UseBasicParsing -Method Get
    if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 300) { Pass "/api/reports/$rid ($($resp.StatusCode))" } else { Fail "/api/reports/$rid ($($resp.StatusCode))" }
  } else {
    Fail "report create"
  }
} catch {
  Fail "report create"
}

Write-Host "-- /api/results --"
try {
  $resp = Invoke-WebRequest -Uri "$BaseUrl/api/results?limit=1" -UseBasicParsing -Method Get
  if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 300) { Pass "/api/results ($($resp.StatusCode))" } else { Fail "/api/results ($($resp.StatusCode))" }
} catch {
  Fail "/api/results"
}

Write-Host "-- /api/results/stream (SSE) --"
try {
  $out = & curl.exe -sN --max-time 5 "$BaseUrl/api/results/stream?limit=5"
  if ($out -match "event:" -or $out -match "data:") { Pass "/api/results/stream stream" } else { Fail "/api/results/stream stream" }
} catch {
  Fail "/api/results/stream stream"
}

Write-Host "-- /api/summary --"
try {
  $resp = Invoke-WebRequest -Uri "$BaseUrl/api/summary" -UseBasicParsing -Method Get
  if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 300) { Pass "/api/summary ($($resp.StatusCode))" } else { Fail "/api/summary ($($resp.StatusCode))" }
} catch {
  Fail "/api/summary"
}

if ($Logs) {
  Write-Host "-- gateway logs (last 50) --"
  cmd /c "docker compose -f $ComposeFile logs --tail=50 gateway"
  $ps = cmd /c "docker compose -f $ComposeFile ps"
  if ($ps -match "crypto-stream") {
    Write-Host "-- crypto-stream logs (last 50) --"
    cmd /c "docker compose -f $ComposeFile logs --tail=50 crypto-stream"
  }
}

Write-Host "== DONE ==" -ForegroundColor Cyan
if ($script:Failed) { exit 1 }
