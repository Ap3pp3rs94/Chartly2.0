$ErrorActionPreference = "Stop"

$repo = "C:\Chartly2.0\Chartly2.0"
$intervalSeconds = 30

Write-Host "git push loop started (interval: $intervalSeconds seconds)"
Write-Host "repo: $repo"
Write-Host "Press Ctrl+C to stop."

while ($true) {
    $ts = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss")
    try {
        Push-Location $repo
        Write-Host "[$ts] git push"
        git push
    } catch {
        Write-Host "[$ts] git push failed: $($_.Exception.Message)"
    } finally {
        Pop-Location
    }
    Start-Sleep -Seconds $intervalSeconds
}
