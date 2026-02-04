# Chartly 2.0  Python SDK (v0)

The Python SDK is intentionally **thin**: it standardizes how Python clients call Chartly services (headers, tracing propagation, error decoding), while Chartly enforces correctness through **contracts** and **profiles**.

## Scope

**This SDK is:**
- A small helper layer for HTTP calls into Chartly services (typically via the Gateway).
- A consistent approach for tenancy headers, request correlation, and trace propagation.
- A consistent way to decode Chartly error envelopes and interpret retry signals.

**This SDK is not:**
- A fully generated client for every endpoint (yet).
- A replacement for contracts/profiles validation.
- A framework that hides service boundaries.

---

## Quickstart (HTTP)

This example shows the correct baseline wiring:
- `X-Tenant-Id` tenant header (recommended)
- `X-Request-Id` request correlation (recommended)
- optional W3C `traceparent` propagation

> This README references only generic endpoints (`/health`, `/ready`) to avoid inventing API surface.

```python
import requests

BASE_URL = "http://localhost:8080"  # example: Gateway base URL
TENANT   = "local"
REQ_ID   = "req_python_example_001"

headers = {
    "X-Tenant-Id": TENANT,
    "X-Request-Id": REQ_ID,
    # Optional tracing propagation:
    # "traceparent": "00-<traceid>-<spanid>-01",
}

r = requests.get(f"{BASE_URL}/health", headers=headers, timeout=10)
print("status:", r.status_code)
print(r.text)
```

```python
r = requests.get(f"{BASE_URL}/ready", headers=headers, timeout=10)
print("status:", r.status_code)
print(r.text)
```

---

## Tenancy headers

Chartly is multi-tenant by design. Most service endpoints require a tenant header:

- Header: `X-Tenant-Id`
- In **local** environments, missing tenant headers may default to `local`.
- In **non-local** environments, tenant headers are typically required.

Always set `X-Tenant-Id` explicitly to avoid ambiguous routing.

---

## Error envelopes

Chartly services return a deterministic error envelope (see `pkg/errors`). It includes:

- `code` (stable error code)
- `message` (human-readable)
- `retryable` (true/false)
- `kind` (client | server | security | dependency)
- optional `request_id` and `trace_id`
- optional `details` (sorted key/value pairs)

Use `retryable` as the primary retry hint; use `code` and `kind` for structured handling.

---

## Tracing propagation

Chartly uses `pkg/telemetry` conventions for tracing. The primary wire format is **W3C Trace Context**:

- Header: `traceparent`
- Optional: `tracestate`

Propagate these headers to preserve trace continuity.

---

## Retry and idempotency guidance

Chartly standardizes queue semantics (`pkg/queue`) and idempotency keys (`pkg/idempotency`).

**Queue semantics**
- At-least-once delivery is expected.
- Consumers should ACK or NACK and handle retries.
- For poison messages, use the DLQ flow when supported.

**Idempotency**
- Use deterministic keys for operations that must not execute twice.
- Keys are of the form: `v1:<tenant>:<scope>:<sha256hex>`
- Scope should be stable and low-cardinality.

---

## Determinism and contracts

This SDK intentionally avoids guessing service behavior. Use:

- **Contracts** (`pkg/contracts`) for payload validation.
- **Profiles** (`pkg/profiles`) for configuration and schema overlays.

Together, these provide stable, deterministic behavior across environments.
