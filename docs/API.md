# Chartly 2.0  API Overview

This document describes the **baseline HTTP conventions** for Chartly services.
The SDKs (Python/TypeScript) implement the same behaviors.

## Baseline Endpoints

Every service SHOULD expose the following endpoints:

- `GET /health`  liveness check (2xx on healthy)
- `GET /ready`   readiness check (2xx when dependencies are ready)

## Required Headers (Recommended)

- `x-request-id`  caller-generated request id (string)
- `x-chartly-tenant`  tenant id (when multi-tenant)
- `traceparent`  W3C trace-context propagation header
- `tracestate`   optional W3C trace-context header

## Error Envelope

Non-2xx responses SHOULD use the Chartly error envelope:

```json
{
  "error": {
    "code": "BAD_REQUEST",
    "message": "Invalid request payload",
    "retryable": false,
    "kind": "client",
    "request_id": "req-123",
    "trace_id": "...",
    "details": [
      { "k": "field", "v": "kind" }
    ]
  }
}
```

SDKs attempt to decode this envelope and raise structured errors.

## Idempotency

For write operations, callers should send a stable request id (`x-request-id`)
that remains constant across retries. A typical format:

```
req_<service>_<yyymmdd>_<random>
```

## Tracing

W3C `traceparent` is always propagated by SDKs. For example:

```
traceparent: 00-0123456789abcdef0123456789abcdef-0123456789abcdef-01
```

## Data Shapes

Request/response schema definitions live under:

- `contracts/v1/**`

These JSON Schemas define shared payloads (reports, telemetry, errors).
