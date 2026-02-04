# scripts/testing

Fast is it alive? testing scripts.

## Env

- `CHARTLY_BASE_URL` (optional)  default: `http://localhost:8080`

## Usage

```powershell
# quick smoke (health/ready)
.\scripts\testing\smoke.ps1

# basic API contract checks
.\scripts\testing\api_contracts.ps1

# simple load ping
.\scripts\testing\load_ping.ps1 -N 50 -Concurrency 10
```
