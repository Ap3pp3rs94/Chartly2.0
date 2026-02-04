<##>
# Smoke test: /health and /ready
# Usage: .\scripts\testing\smoke.ps1 [-BaseUrl http://localhost:8080]
<##>

[CmdletBinding()]
param(
  [string]$BaseUrl = $env:CHARTLY_BASE_URL
)

function Say([string]$Message) { Write-Host ("[smoke] " + $Message) }
function Fail([string]$Message) { Write-Error $Message; exit 1 }

if ([string]::IsNullOrWhiteSpace($BaseUrl)) { $BaseUrl = "http://localhost:8080" }
$BaseUrl = $BaseUrl.TrimEnd('/')

$health = "$BaseUrl/health"
$ready  = "$BaseUrl/ready"

Say "GET $health"
try {
  $h = Invoke-WebRequest -Uri $health -UseBasicParsing -TimeoutSec 10
  Say "health: $($h.StatusCode)"
} catch {
  Fail "health failed: $($_.Exception.Message)"
}

Say "GET $ready"
try {
  $r = Invoke-WebRequest -Uri $ready -UseBasicParsing -TimeoutSec 10
  Say "ready: $($r.StatusCode)"
} catch {
  Fail "ready failed: $($_.Exception.Message)"
}

Say "done"
