$ErrorActionPreference = "Stop"

$Source = "C:\Chartly2.0"
$Dest   = "C:\Chartly2.0\Chartly2.0"
$Log    = "C:\Chartly2.0\scripts\win\auto_sync.log"

function Log($msg) {
  $ts = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss")
  Add-Content -LiteralPath $Log -Value "$ts $msg"
}

try {
  # Sync files (exclude the clone itself and any .git directory)
  $null = & robocopy $Source $Dest /E /XD $Dest (Join-Path $Source ".git") (Join-Path $Source "Chartly2.0") /R:1 /W:1

  Set-Location $Dest

  # Only commit/push when there are changes
  $status = git status --porcelain
  if ([string]::IsNullOrWhiteSpace($status)) {
    Log "No changes."
    exit 0
  }

  git add -A | Out-Null

  $msg = "Auto sync " + (Get-Date -Format "yyyy-MM-dd HH:mm:ss")
  git commit -m $msg | Out-Null

  git push origin main | Out-Null

  Log "Synced and pushed."
}
catch {
  Log ("ERROR: " + $_.Exception.Message)
  exit 1
}
