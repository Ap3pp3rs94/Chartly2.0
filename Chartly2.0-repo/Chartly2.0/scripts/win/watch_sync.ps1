$ErrorActionPreference = "Stop"

$Source = "C:\Chartly2.0"
$Dest   = "C:\Chartly2.0\Chartly2.0"
$Log    = "C:\Chartly2.0\scripts\win\auto_sync.log"
$DebounceSeconds = 15
$InitialSync = $true

function Log($msg) {
  $ts = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss")
  Add-Content -LiteralPath $Log -Value "$ts $msg"
}

function ShouldIgnorePath($path) {
  if ($path -like "$Dest*") { return $true }
  if ($path -like "*\\.git\\*") { return $true }
  if ($path -like $Log) { return $true }
  return $false
}

function Sync-Repo {
  try {
    # Sync files (exclude the clone itself and any .git directory)
    $null = & robocopy $Source $Dest /E /XD $Dest (Join-Path $Source ".git") (Join-Path $Source "Chartly2.0") /R:1 /W:1

    Set-Location $Dest

    $status = git status --porcelain
    if ([string]::IsNullOrWhiteSpace($status)) {
      Log "No changes."
      return
    }

    git add -A | Out-Null
    $msg = "Auto sync " + (Get-Date -Format "yyyy-MM-dd HH:mm:ss")
    git commit -m $msg | Out-Null
    git push origin main | Out-Null

    Log "Synced and pushed."
  }
  catch {
    Log ("ERROR: " + $_.Exception.Message)
  }
}

function OnChange($path, $changeType) {
  if (ShouldIgnorePath $path) { return }
  if (-not $script:LastChange) {
    Log ("Change detected: {0} {1}" -f $changeType, $path)
  }
  $script:LastChange = Get-Date
}

$script:LastChange = $null

$watcher = New-Object System.IO.FileSystemWatcher $Source -Property @{
  IncludeSubdirectories = $true
  EnableRaisingEvents   = $false
  NotifyFilter          = [IO.NotifyFilters]"FileName, DirectoryName, LastWrite, Size"
}
# Increase buffer to reduce overflow during bursts of file changes
$watcher.InternalBufferSize = 65536

$action = { OnChange $Event.SourceEventArgs.FullPath $Event.SourceEventArgs.ChangeType }
$errorAction = {
  $ex = $Event.SourceEventArgs.GetException()
  $msg = if ($null -ne $ex) { $ex.Message } else { "Unknown watcher error" }
  Log ("Watcher error: " + $msg)
  # Force a sync attempt after errors to re-establish state
  $script:LastChange = Get-Date
}

Register-ObjectEvent $watcher Changed -Action $action | Out-Null
Register-ObjectEvent $watcher Created -Action $action | Out-Null
Register-ObjectEvent $watcher Deleted -Action $action | Out-Null
Register-ObjectEvent $watcher Renamed -Action $action | Out-Null
Register-ObjectEvent $watcher Error -Action $errorAction | Out-Null

$watcher.EnableRaisingEvents = $true
Log "Watcher started."

if ($InitialSync) {
  Sync-Repo
}

while ($true) {
  Start-Sleep -Seconds 5
  if ($script:LastChange -and ((Get-Date) - $script:LastChange).TotalSeconds -ge $DebounceSeconds) {
    $script:LastChange = $null
    Sync-Repo
  }
}
