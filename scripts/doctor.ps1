<[
.SYNOPSIS
  Sanity checks for Chartly 2.0 dev environment.

.USAGE
  .\scripts\doctor.ps1
]>
param()

Write-Host "== Chartly doctor =="
Write-Host "cwd: $((Get-Location).Path)"

function Check-Cmd([string]$name) {
  $cmd = Get-Command $name -ErrorAction SilentlyContinue
  if ($cmd) { "ok  $name -> $($cmd.Source)" } else { "miss $name" }
}

Check-Cmd "git"
Check-Cmd "docker"
Check-Cmd "docker-compose"
Check-Cmd "node"
Check-Cmd "npm"
Check-Cmd "go"

# repo sanity
if (Test-Path -LiteralPath ".git") {
  Write-Host "repo: .git present"
} else {
  Write-Host "repo: .git missing (are you in repo root?)"
}

# quick port checks (optional)
Write-Host "ports:"
Write-Host "  8080 ->" (Test-NetConnection -ComputerName 127.0.0.1 -Port 8080 -InformationLevel Quiet)
Write-Host "  5432 ->" (Test-NetConnection -ComputerName 127.0.0.1 -Port 5432 -InformationLevel Quiet)
Write-Host "  6379 ->" (Test-NetConnection -ComputerName 127.0.0.1 -Port 6379 -InformationLevel Quiet)
