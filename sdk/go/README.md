# Chartly 2.0  Go SDK (v0)

This SDK is intentionally **thin**: it standardizes how Go clients call Chartly services (headers, tracing, error decoding), while Chartly enforces correctness through **contracts** and **profiles**.

## What this SDK is

- A small helper layer for HTTP calls into Chartly services (typically via the Gateway).
- A consistent place to set tenancy, request correlation, and trace propagation headers.
- A consistent place to decode error envelopes and map error codes to retry decisions.

## What this SDK is not

- A fully generated client for every endpoint (yet).
- A replacement for contracts or profile validation.
- A framework that hides service boundaries.

---

## Install

From your Go module:

```bash
go get github.com/Ap3pp3rs94/Chartly2.0/sdk/go@latest
```

---

## Quickstart (generic)

Below is a minimal request pattern that only relies on **/health** and **/ready** endpoints, which are expected across Chartly services.

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    "time"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    baseURL := "http://localhost:8080" // example only

    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
    // Optional: add request/trace headers here (see Tracing section)

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    fmt.Println("status:", resp.StatusCode)
}
```

> Note: This SDK does not assume service-specific endpoints. Production clients should use service-specific contracts/profiles for payload shape and validation.

---

## Tenancy headers

Chartly is multi-tenant by design. Most service endpoints require a tenant header.

- Header: `X-Tenant-Id`
- In **local** environments, missing tenant headers may default to `local`.
- In **non-local** environments, tenant headers are typically required.

Your client code should always set `X-Tenant-Id` explicitly to avoid ambiguity.

---

## Error envelopes

Chartly services return a deterministic error envelope (see `pkg/errors`). It includes:

- `code` (stable error code)
- `message` (human-readable)
- `retryable` (true/false)
- `kind` (client | server | security | dependency)
- optional `request_id` and `trace_id`
- optional `details` (sorted key/value pairs)

Your client should treat `retryable` as the primary hint for retries, with `code` and `kind` for structured handling.

---

## Tracing propagation

Chartly uses `pkg/telemetry` conventions for tracing. The primary wire format is **W3C Trace Context**:

- Header: `traceparent`
- Optional: `tracestate`

When making outbound requests, propagate your current trace context into these headers. If you do not have a trace context, generate a new one at the application boundary.

---

## Queue and idempotency guidance

Chartly standardizes queue semantics (`pkg/queue`) and idempotency keys (`pkg/idempotency`).

**Queue**

- At-least-once delivery is expected.
- Consumers should ACK or NACK and handle retries.
- For poison messages, use the DLQ flow when supported.

**Idempotency**

- Use deterministic keys for operations that must not execute twice.
- Keys are of the form: `v1:<tenant>:<scope>:<sha256hex>`
- Scope should be a stable, low-cardinality token.

---

## Determinism and contracts

This SDK intentionally avoids guessing service behavior. Use:

- **Contracts** (`pkg/contracts`) for payload validation.
- **Profiles** (`pkg/profiles`) for configuration and schema overlays.

Together, these provide stable, deterministic behavior across environments.
