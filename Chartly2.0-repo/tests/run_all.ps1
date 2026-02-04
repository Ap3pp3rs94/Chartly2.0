<##>
# Run all tests (best-effort harness)
# Usage: .\tests\run_all.ps1
<##>

[CmdletBinding()]
param()

function Say([string]$Message) { Write-Host ("[tests] " + $Message) }
function Fail([string]$Message) { Write-Error $Message }

$repo = Split-Path -Parent $PSScriptRoot

$failures = 0

function Run-Step([string]$Name, [string]$Cmd) {
  Say "Running: $Name"
  try {
    Invoke-Expression $Cmd
    if ($LASTEXITCODE -ne 0) { throw "exit $LASTEXITCODE" }
    Say "OK: $Name"
  } catch {
    $script:failures++
    Fail "FAIL: $Name ($($_.Exception.Message))"
  }
}

# Smoke + contracts
$smoke = Join-Path $repo "scripts\testing\smoke.ps1"
if (Test-Path $smoke) { Run-Step "smoke" ("& `"{0}`"" -f $smoke) } else { Say "skip: smoke.ps1 missing" }

$contracts = Join-Path $repo "scripts\testing\api_contracts.ps1"
if (Test-Path $contracts) { Run-Step "api_contracts" ("& `"{0}`"" -f $contracts) } else { Say "skip: api_contracts.ps1 missing" }

$load = Join-Path $repo "scripts\testing\load_ping.ps1"
if (Test-Path $load) { Run-Step "load_ping" ("& `"{0}`" -N 10 -Concurrency 2" -f $load) } else { Say "skip: load_ping.ps1 missing" }

# Optional sh integration test
$int = Join-Path $repo "scripts\testing\integration_test.sh"
$sh = Get-Command sh -ErrorAction SilentlyContinue
if ($sh -and (Test-Path $int)) {
  Run-Step "integration_test.sh" ("& `"{0}`" `"{1}`"" -f $sh.Source, $int)
} else {
  Say "skip: integration_test.sh (sh or file missing)"
}

# Unit test orchestrator
$unit = Join-Path $repo "tests\unit\run_unit.ps1"
if (Test-Path $unit) { Run-Step "unit" ("& `"{0}`"" -f $unit) } else { Say "skip: unit/run_unit.ps1 missing" }

Say "Summary: failures=$failures"
if ($failures -ne 0) { exit 1 }
exit 0
