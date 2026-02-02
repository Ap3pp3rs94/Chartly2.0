<[
.SYNOPSIS
  Load env vars from .env.local into current session (if present).

.USAGE
  .\scripts\dev\env-load.ps1
]>
param()

$envFile = "C:\Chartly2.0\.env.local"
Write-Host "== env-load.ps1 =="
Write-Host "file: $envFile"

if (-not (Test-Path -LiteralPath $envFile)) {
  Write-Host "skip: .env.local missing"
  exit 0
}

Get-Content -LiteralPath $envFile | ForEach-Object {
  $line = $_.Trim()
  if ($line -eq "" -or $line.StartsWith("#")) { return }
  $parts = $line -split "=", 2
  if ($parts.Count -ne 2) { return }
  $key = $parts[0].Trim()
  $val = $parts[1].Trim().Trim('"').Trim("'")
  if ($key -ne "") {
    $env:$key = $val
  }
}

Write-Host "loaded env vars from .env.local"
