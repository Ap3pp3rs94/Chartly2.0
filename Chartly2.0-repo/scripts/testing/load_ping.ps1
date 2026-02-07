<##>
# Simple concurrent load ping to /health
# Usage: .\scripts\testing\load_ping.ps1 [-BaseUrl http://localhost:8080] [-N 50] [-Concurrency 10]
<##>

[CmdletBinding()]
param(
  [string]$BaseUrl = $env:CHARTLY_BASE_URL,
  [int]$N = 50,
  [int]$Concurrency = 10
)

function Say([string]$Message) { Write-Host ("[load] " + $Message) }
function Fail([string]$Message) { Write-Error $Message; exit 1 }

if ([string]::IsNullOrWhiteSpace($BaseUrl)) { $BaseUrl = "http://localhost:8080" }
$BaseUrl = $BaseUrl.TrimEnd('/')
if ($N -le 0) { Fail "N must be > 0" }
if ($Concurrency -le 0) { Fail "Concurrency must be > 0" }

$url = "$BaseUrl/health"

Say "url=$url N=$N concurrency=$Concurrency"

$success = 0
$fail = 0
$start = Get-Date

$jobs = New-Object System.Collections.ArrayList

for ($i = 0; $i -lt $N; $i++) {
  while ($jobs.Count -ge $Concurrency) {
    $done = $jobs | Where-Object { $_.State -ne 'Running' }
    foreach ($j in $done) {
      $res = Receive-Job $j -ErrorAction SilentlyContinue
      if ($res -eq 1) { $success++ } else { $fail++ }
      Remove-Job $j | Out-Null
      [void]$jobs.Remove($j)
    }
    Start-Sleep -Milliseconds 50
  }

  $job = Start-Job -ScriptBlock {
    param($u)
    try {
      $r = Invoke-WebRequest -Uri $u -UseBasicParsing -TimeoutSec 10
      if ($r.StatusCode -eq 200) { return 1 } else { return 0 }
    } catch { return 0 }
  } -ArgumentList $url

  [void]$jobs.Add($job)
}

while ($jobs.Count -gt 0) {
  $done = $jobs | Where-Object { $_.State -ne 'Running' }
  foreach ($j in $done) {
    $res = Receive-Job $j -ErrorAction SilentlyContinue
    if ($res -eq 1) { $success++ } else { $fail++ }
    Remove-Job $j | Out-Null
    [void]$jobs.Remove($j)
  }
  Start-Sleep -Milliseconds 50
}

$elapsed = (Get-Date) - $start

Say "success=$success fail=$fail elapsed=$($elapsed.TotalSeconds.ToString('0.00'))s"

if ($fail -gt 0) {
  Fail "Some requests failed"
}

Say "done"
