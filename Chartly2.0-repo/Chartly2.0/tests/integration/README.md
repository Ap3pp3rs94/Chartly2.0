# /tests/integration

PowerShell-first integration harness.

## Usage

```powershell
# Run smoke + integration scripts (requires sh for shell tests)
.\tests\integration\run_integration.ps1

# With seed (requires -Yes)
.\tests\integration\run_integration.ps1 -Seed -Yes
```

## Environment

- `CHARTLY_BASE_URL` (optional)  default: `http://localhost:8080`
- `CHARTLY_SH` (optional)  shell command or path (e.g., Git Bash sh.exe)
