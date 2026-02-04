# Chartly 2.0  Scripts

PowerShell-first helper scripts for local development.

## Conventions

- Scripts are idempotent (safe to run multiple times).
- No secrets committed; use `.env` files and/or environment variables.
- Prefer explicit errors over silent failures.

## Quick start

```powershell
# 1) Verify toolchain + repo sanity
.\scripts\doctor.ps1

# 2) Load env vars from .env files
.\scripts\env.ps1

# 3) Start local stack (docker compose)
.\scripts\up.ps1

# 4) Smoke test gateway endpoints
.\scripts\smoke.ps1 -BaseUrl http://localhost:8080
```

## Scripts

- `doctor.ps1`  System + repo sanity checks
- `env.ps1`     Loads `.env` files into current session
- `up.ps1`      Bring up local containers (docker compose)
- `down.ps1`    Stop local containers
- `smoke.ps1`   Basic /health + /ready checks
