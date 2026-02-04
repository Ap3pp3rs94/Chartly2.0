# /tests  Chartly 2.0

This folder is the top-level test harness. It does not duplicate service-specific unit tests.
Instead, it orchestrates:

- smoke tests (health/ready)
- integration tests (optional seed + optional events)
- SDK build checks (optional hooks)
- future unit test runners (Go, Python, TS)

## Quick run (Windows)

```powershell
# From repo root
.\tests\run_all.ps1
```

## Environment

- `CHARTLY_BASE_URL` (optional)  default: `http://localhost:8080`
- `CHARTLY_TENANT_ID` (optional)
- `CHARTLY_REQUEST_ID` (optional)
- `CHARTLY_EVENTS_PATH` (optional)

## What it runs

- `scripts/testing/smoke.ps1`
- `scripts/testing/api_contracts.ps1`
- `scripts/testing/load_ping.ps1` (short default)
- `scripts/testing/integration_test.sh` (optional if sh + curl exist)
- `tests/unit/run_unit.ps1`
