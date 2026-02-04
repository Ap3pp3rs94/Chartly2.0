# scripts/deploy

Safe, staged deployment helpers.

## Safety gates

- You must pass `-Env dev|staging|prod` (no default).
- `prod` requires BOTH:
  - `-Yes`
  - `CHARTLY_ALLOW_PROD_DEPLOY=1`

No secrets are stored in this repo. Use environment variables or your secret manager.

## Typical usage

```powershell
# See what would happen
.\scripts\deploy\plan.ps1 -Env staging

# Deploy to staging
.\scripts\deploy\deploy.ps1 -Env staging

# Deploy to prod (requires explicit allow)
$env:CHARTLY_ALLOW_PROD_DEPLOY="1"
.\scripts\deploy\deploy.ps1 -Env prod -Yes
```

## Files

- `deploy.ps1`  Orchestrates build/push/apply (placeholders to wire your CI/CD).
- `plan.ps1`    Prints steps with no side effects.
- `env-template.ps1`  Documents required environment variables.
