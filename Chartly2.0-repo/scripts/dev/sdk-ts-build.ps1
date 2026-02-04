<[
.SYNOPSIS
  Build the TypeScript SDK (sdk\typescript) if package.json exists.

.USAGE
  .\scripts\dev\sdk-ts-build.ps1
]>
param()

Write-Host "== sdk-ts-build.ps1 =="
$dir = "C:\Chartly2.0\sdk\typescript"
$pkg = Join-Path $dir "package.json"

if (-not (Test-Path -LiteralPath $pkg)) {
  Write-Host "skip: package.json not found"
  exit 0
}

Push-Location $dir
try {
  npm run build
}
finally {
  Pop-Location
}
