# Chartly 2.0  TypeScript SDK (v0)

This SDK is intentionally **thin**: it standardizes how TypeScript clients call Chartly services (headers, tracing propagation, error decoding), while Chartly enforces correctness through **contracts** and **profiles**.

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

```ts
const BASE_URL = "http://localhost:8080"; // example: Gateway base URL
const TENANT = "local";
const REQUEST_ID = "req_ts_example_001";

const headers: Record<string, string> = {
  "X-Tenant-Id": TENANT,
  "X-Request-Id": REQUEST_ID,
  // Optional tracing propagation:
  // "traceparent": "00-<traceid>-<spanid>-01",
};

const res = await fetch(`${BASE_URL}/health`, { headers });
console.log("status:", res.status);
console.log(await res.text());
```

## Tenancy header behavior

Chartly uses a tenant header (default `X-Tenant-Id`) to route requests. In local/dev environments the platform may accept a default tenant like `local`. In non-local environments, you should treat tenant headers as **required** and ensure they are explicitly set for each request.

## Error envelope conventions

When a service returns a non-2xx response, Chartly emits a standard error envelope shaped like:

```json
{
  "error": {
    "code": "config.invalid",
    "message": "...",
    "retryable": false,
    "kind": "client",
    "request_id": "...",
    "trace_id": "...",
    "details": [ { "k": "field", "v": "reason" } ]
  }
}
```

The canonical definitions live in `pkg/errors`. Clients should read `code`, `retryable`, and `kind` to decide whether to retry or surface the error to a user.

## Tracing propagation (W3C)

Chartly uses W3C trace context headers. If you have an inbound trace, forward it:

- `traceparent` (required to propagate)
- `tracestate` (optional)

You can generate new trace IDs when starting a fresh workflow, but should propagate existing headers whenever possible.

## Retry + idempotency guidance

For retries, use stable idempotency keys to avoid duplicated side effects. The canonical key format is:

```
v1:<tenant>:<scope>:<sha256hex>
```

Scope should be a short stable token (for example: `job`, `event`, `ingest`). The hash should be built from a deterministic encoding of your inputs. See `pkg/idempotency` for helpers.

## Notes

- This SDK intentionally avoids inventing service-specific endpoints.
- Use `/health` and `/ready` for simple integration checks.
- For complex workflows, rely on Chartly contracts and profiles rather than client-side heuristics.
