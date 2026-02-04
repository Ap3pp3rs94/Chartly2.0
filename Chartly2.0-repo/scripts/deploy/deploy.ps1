<##>
# Chartly 2.0 deploy orchestrator
#
# Usage:
#   .\scripts\deploy\deploy.ps1 -Env dev|staging|prod [-Yes]
#
# Safety:
#   - Requires explicit -Env
#   - prod requires -Yes and CHARTLY_ALLOW_PROD_DEPLOY=1
<##>

[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [ValidateSet('dev','staging','prod')]
  [string]$Env,

  [switch]$Yes
)

function Fail([string]$Message) {
  Write-Error $Message
  exit 1
}

function Say([string]$Message) {
  Write-Host ("[deploy] " + $Message)
}

# Safety gates
if ($Env -eq 'prod') {
  if (-not $Yes) {
    Fail "Refusing prod deploy without -Yes"
  }
  if ($env:CHARTLY_ALLOW_PROD_DEPLOY -ne '1') {
    Fail "Refusing prod deploy without CHARTLY_ALLOW_PROD_DEPLOY=1"
  }
}

Say "env=$Env"

# Optional env variables (documented in env-template.ps1)
$registry = $env:CHARTLY_REGISTRY
$tag = $env:CHARTLY_IMAGE_TAG
$cluster = $env:CHARTLY_CLUSTER
$namespace = $env:CHARTLY_NAMESPACE

# Placeholder hooks (safe no-ops unless you wire them)
Say "Step 1: build artifacts (placeholder)"
# Example: .\scripts\build.ps1 -Env $Env

Say "Step 2: push images (placeholder)"
# Example: docker push $registry/my-service:$tag

Say "Step 3: apply deployment (placeholder)"
# Example: kubectl -n $namespace apply -f k8s/$Env

Say "done"
