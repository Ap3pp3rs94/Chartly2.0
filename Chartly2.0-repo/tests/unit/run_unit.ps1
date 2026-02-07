<##>
# Run unit tests (best-effort)
# Usage: .\tests\unit\run_unit.ps1
<##>

[CmdletBinding()]
param()

function Say([string]$Message) { Write-Host ("[unit] " + $Message) }
function Fail([string]$Message) { Write-Error $Message }

$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
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

# Go tests (if go + go.mod exists)
$go = Get-Command go -ErrorAction SilentlyContinue
$goMod = Get-ChildItem -LiteralPath $repo -Recurse -Filter go.mod -ErrorAction SilentlyContinue | Select-Object -First 1
if ($go -and $goMod) {
  Run-Step "go test ./..." ("& `"{0}`" test ./..." -f $go.Source)
} else {
  Say "skip: go tests (go or go.mod missing)"
}

# TypeScript tests (if package.json exists under sdk/typescript)
$pkg = Join-Path $repo "sdk\typescript\package.json"
if (Test-Path $pkg) {
  Run-Step "ts test" "npm --prefix sdk\\typescript test"
} else {
  Say "skip: ts tests (package.json missing)"
}

# Python tests (if sdk/python exists)
$py = Get-Command python -ErrorAction SilentlyContinue
$pyRoot = Join-Path $repo "sdk\python"
if ($py -and (Test-Path $pyRoot)) {
  Run-Step "python -m pytest" "python -m pytest"
} else {
  Say "skip: python tests (python or sdk/python missing)"
}

Say "Summary: failures=$failures"
if ($failures -ne 0) { exit 1 }
exit 0
