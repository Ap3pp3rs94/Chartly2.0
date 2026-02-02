param(
  [string]$BaseUrl = "",
  [string]$Endpoint = "/health",
  [int]$N = 100,
  [int]$C = 10,
  [int]$TimeoutSec = 10
)

$ErrorActionPreference = "Stop"

function Say($msg) { Write-Host "[load] $msg" }

# Safety gate: explicit opt-in
if (($env:CHARTLY_LOAD ?? "") -ne "1") {
  throw "Refusing to run load test: set CHARTLY_LOAD=1 to enable."
}

if (-not $BaseUrl) {
  $BaseUrl = ($env:CHARTLY_BASE_URL ?? "").Trim()
}
if (-not $BaseUrl) {
  throw "Base URL required. Provide -BaseUrl or set CHARTLY_BASE_URL."
}

# Resolve sh (PowerShell-first portability):
# - CHARTLY_SH override (path or command name)
# - fallback: sh on PATH
$shCmd = ($env:CHARTLY_SH ?? "").Trim()
if (-not $shCmd) { $shCmd = "sh" }

$sh = Get-Command $shCmd -ErrorAction SilentlyContinue
if ($null -eq $sh) {
  throw "Shell not found ('$shCmd'). Install Git Bash or WSL and ensure 'sh' is on PATH, or set CHARTLY_SH."
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\.."))
$script = Join-Path $repoRoot "scripts\testing\load_test.sh"

if (!(Test-Path -LiteralPath $script)) {
  throw "Missing load test script: $script"
}

# Normalize endpoint
$Endpoint = $Endpoint.Trim()
if (-not $Endpoint.StartsWith("/")) { $Endpoint = "/" + $Endpoint }

# Validate inputs
if ($N -lt 1) { throw "-N must be >= 1" }
if ($C -lt 1) { throw "-C must be >= 1" }
if ($TimeoutSec -lt 1) { throw "-TimeoutSec must be >= 1" }

Say "BaseUrl:   $BaseUrl"
Say "Endpoint:  $Endpoint"
Say "Requests:  $N"
Say "Concurrency: $C"
Say "Timeout:   ${TimeoutSec}s"
Say "Shell:     $($sh.Source)"

& $shCmd $script --base-url $BaseUrl --endpoint $Endpoint --n $N --c $C --timeout $TimeoutSec
