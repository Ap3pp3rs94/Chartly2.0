param(
  [string]$BaseUrl = "",
  [switch]$Seed,
  [switch]$Yes
)

$ErrorActionPreference = "Stop"

function Say($msg) { Write-Host "[integration] $msg" }

# SAFETY: seeding is mutating; require explicit -Yes at orchestration layer.
if ($Seed -and -not $Yes) {
  throw "Refusing to run with -Seed without -Yes (mutating operation)."
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path

if (-not $BaseUrl) {
  $BaseUrl = ($env:CHARTLY_BASE_URL ?? "").Trim()
}
if (-not $BaseUrl) {
  $BaseUrl = "http://localhost:8080"
}

$env:CHARTLY_BASE_URL = $BaseUrl

# Resolve sh (PowerShell-first portability):
# - If CHARTLY_SH is set, use it (path or command name).
# - Otherwise, try "sh" on PATH.
$shCmd = ($env:CHARTLY_SH ?? "").Trim()
if (-not $shCmd) { $shCmd = "sh" }

$sh = Get-Command $shCmd -ErrorAction SilentlyContinue
if ($null -eq $sh) {
  throw "Shell not found ('$shCmd'). Install Git Bash or WSL and ensure 'sh' is on PATH, or set CHARTLY_SH to the shell command/path."
}

Say "RepoRoot: $repoRoot"
Say "BaseUrl:  $BaseUrl"
Say "Seed:     $Seed"
Say "Shell:    $($sh.Source)"

# Integration is orchestration; call existing, real scripts.
$smoke = Join-Path $repoRoot "scripts\testing\smoke_test.sh"
$it    = Join-Path $repoRoot "scripts\testing\integration_test.sh"

if (!(Test-Path -LiteralPath $smoke)) {
  throw "Missing smoke test script: $smoke"
}

Say "Running smoke test..."
& $shCmd $smoke --base-url $BaseUrl

if (Test-Path -LiteralPath $it) {
  $args = @("--base-url", $BaseUrl)
  if ($Seed) { $args += "--seed" }
  if ($Yes)  { $args += "--yes" }

  Say "Running integration test..."
  & $shCmd $it @args
} else {
  Say "No integration_test.sh found (ok for early stage)"
}

Say "integration tests complete"
