<[
.SYNOPSIS
  Load environment variables from .env files into the current PowerShell session.

.DESCRIPTION
  This script is idempotent and safe. It supports multiple .env files and ignores
  blank lines and comments. Lines are parsed as KEY=VALUE with optional quotes.

.USAGE
  .\scripts\env.ps1 -Paths ".env",".env.local"
]>
param(
  [string[]]$Paths = @(".env", ".env.local")
)

Write-Host "== env.ps1 =="

$loaded = 0
foreach ($p in $Paths) {
  if (-not (Test-Path -LiteralPath $p)) {
    Write-Host "skip: $p (missing)"
    continue
  }
  Write-Host "load: $p"
  Get-Content -LiteralPath $p | ForEach-Object {
    $line = $_.Trim()
    if ($line -eq "" -or $line.StartsWith("#")) { return }
    $parts = $line -split "=", 2
    if ($parts.Count -ne 2) { return }
    $key = $parts[0].Trim()
    $val = $parts[1].Trim().Trim('"').Trim("'")
    if ($key -ne "") {
      $env:$key = $val
      $loaded++
    }
  }
}

Write-Host "loaded $loaded variables"
