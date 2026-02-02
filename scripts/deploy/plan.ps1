<##>
# Chartly 2.0 deploy plan (no side effects)
#
# Usage:
#   .\scripts\deploy\plan.ps1 -Env dev|staging|prod
<##>

[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [ValidateSet('dev','staging','prod')]
  [string]$Env
)

function Say([string]$Message) {
  Write-Host ("[plan] " + $Message)
}

Say "env=$Env"
Say "Would run: build artifacts"
Say "Would run: push images"
Say "Would run: apply deployment"
Say "No changes made"
