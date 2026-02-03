# /tests/unit

This folder is a cross-language unit test scaffold.

## Organization

- `go/`  Go unit tests (usually live next to packages, but this can host shared helpers)
- `typescript/`  TypeScript unit tests (Jest/Vitest/etc; planned)
- `python/`  Python unit tests (pytest; planned)

Chartly services should normally keep unit tests near the code they test.
This folder exists for:
- shared test utilities
- repo-wide unit test orchestration
- future consolidated CI steps

## Running

```powershell
# best-effort orchestrator (skips what isn't configured)
.\tests\unit\run_unit.ps1
```
