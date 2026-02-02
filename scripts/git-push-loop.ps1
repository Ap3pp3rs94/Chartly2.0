param(
    [int]$IntervalSec = 30,
    [string]$Remote = "origin",
    [string]$Branch = ""
)

$ErrorActionPreference = "Stop"

function Say($msg) { Write-Host "[git-push-loop] $msg" }
function Die($msg) { Write-Host "[git-push-loop] ERROR: $msg" -ForegroundColor Red; exit 1 }

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if (-not (Test-Path -LiteralPath (Join-Path $repoRoot ".git"))) {
    Die "No .git directory found at repo root: $repoRoot"
}

if ($IntervalSec -lt 5) {
    Die "IntervalSec must be >= 5"
}

# Resolve branch if not provided
if ([string]::IsNullOrWhiteSpace($Branch)) {
    try {
        $Branch = (& git -C $repoRoot rev-parse --abbrev-ref HEAD).Trim()
    } catch {
        Die "Failed to resolve git branch. Is git installed and on PATH?"
    }
}

Say "repo_root: $repoRoot"
Say "remote:    $Remote"
Say "branch:    $Branch"
Say "interval:  ${IntervalSec}s"
Say "Press Ctrl+C to stop."

while ($true) {
    $ts = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss")
    Say "push attempt at $ts"
    try {
        & git -C $repoRoot push $Remote $Branch
    } catch {
        Say "push failed: $($_.Exception.Message)"
    }
    Start-Sleep -Seconds $IntervalSec
}
