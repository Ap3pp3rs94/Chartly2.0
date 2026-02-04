# /tests/load  Chartly 2.0

This directory defines **load testing orchestration**, not load test logic.

## What belongs here

Load tests exercise:
- throughput
- latency under concurrency
- system behavior under sustained pressure

They are **not** unit tests and **not** correctness tests.

## Philosophy

- Load testing is **opt-in** and potentially destructive.
- We reuse proven scripts instead of duplicating logic.
- This directory exists to:
  - document how to run load tests
  - provide a single, safe entrypoint

## Current implementation

Load testing logic lives in:
- `scripts/testing/load_test.sh`

This directory **calls** that script with guardrails.

## Safety

To run load tests you MUST:
- explicitly set `CHARTLY_LOAD=1`
- provide a base URL (param or env)

This prevents accidental execution in CI or local dev.

## Quick start

```powershell
$env:CHARTLY_LOAD="1"
$env:CHARTLY_BASE_URL="http://localhost:8080"

.\tests\load\run_load.ps1 -N 500 -C 25 -Endpoint "/health" -TimeoutSec 10
```
