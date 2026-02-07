<##>
# API contract checks (basic)
# Usage: .\scripts\testing\api_contracts.ps1 [-BaseUrl http://localhost:8080]
<##>

[CmdletBinding()]
param(
  [string]$BaseUrl = $env:CHARTLY_BASE_URL
)

function Say([string]$Message) { Write-Host ("[contracts] " + $Message) }
function Fail([string]$Message) { Write-Error $Message; exit 1 }

if ([string]::IsNullOrWhiteSpace($BaseUrl)) { $BaseUrl = "http://localhost:8080" }
$BaseUrl = $BaseUrl.TrimEnd('/')

# Health should be JSON object with service/overall/hash
$healthUrl = "$BaseUrl/health"
Say "GET $healthUrl"
try {
  $resp = Invoke-WebRequest -Uri $healthUrl -UseBasicParsing -TimeoutSec 10
  if ($resp.StatusCode -ne 200) { Fail "health status not 200: $($resp.StatusCode)" }
  $json = $resp.Content | ConvertFrom-Json
  if (-not $json.service) { Fail "health missing .service" }
  if (-not $json.overall) { Fail "health missing .overall" }
  if (-not $json.hash) { Fail "health missing .hash" }
  Say "health contract ok"
} catch {
  Fail "health contract failed: $($_.Exception.Message)"
}

# Ready should return 200 (shape not enforced)
$readyUrl = "$BaseUrl/ready"
Say "GET $readyUrl"
try {
  $resp = Invoke-WebRequest -Uri $readyUrl -UseBasicParsing -TimeoutSec 10
  if ($resp.StatusCode -ne 200) { Fail "ready status not 200: $($resp.StatusCode)" }
  Say "ready status ok"
} catch {
  Fail "ready contract failed: $($_.Exception.Message)"
}

Say "done"
