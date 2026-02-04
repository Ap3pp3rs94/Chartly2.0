<[
.SYNOPSIS
  Stop local stack (delegates to scripts\down.ps1).

.USAGE
  .\scripts\dev\stack-down.ps1
]>
param()

Write-Host "== stack-down.ps1 =="
$down = "C:\Chartly2.0\scripts\down.ps1"
if (-not (Test-Path -LiteralPath $down)) {
  throw "missing: $down"
}

& $down
