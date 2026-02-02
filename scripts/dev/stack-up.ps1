<[
.SYNOPSIS
  Start local stack (delegates to scripts\up.ps1).

.USAGE
  .\scripts\dev\stack-up.ps1
]>
param()

Write-Host "== stack-up.ps1 =="
$up = "C:\Chartly2.0\scripts\up.ps1"
if (-not (Test-Path -LiteralPath $up)) {
  throw "missing: $up"
}

& $up
