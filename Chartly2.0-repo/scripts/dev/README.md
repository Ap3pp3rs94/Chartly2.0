# scripts/dev

Developer loop helpers (one command conveniences).

## Environment

- `CHARTLY_BASE_URL` (optional)  default: `http://localhost:8080`
- `CHARTLY_TENANT_ID` (optional)
- `CHARTLY_REQUEST_ID` (optional)
- Optional local `.env` file:
  - Create at `C:\Chartly2.0\.env.local` (recommended)
  - This repo should ignore it (no secrets committed)

## Quick usage

```powershell
# Load env vars from .env.local into current session (if present)
.\scripts\dev\env-load.ps1

# Start stack (delegates to scripts\up.ps1)
.\scripts\dev\stack-up.ps1

# Stop stack
.\scripts\dev\stack-down.ps1

# Smoke test default base url
.\scripts\dev\smoke.ps1

# Build TypeScript SDK (if package.json exists)
.\scripts\dev\sdk-ts-build.ps1
```
