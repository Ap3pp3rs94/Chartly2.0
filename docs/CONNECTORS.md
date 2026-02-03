# Chartly 2.0  Connectors

## Contract status & trust model

This document defines **how external data enters Chartly** via Connectors and the rules that make ingestion safe, observable, and deterministic.

### Legend
-  **Implemented**  verified in code and/or conformance tests
- 🛠 **Planned**  desired contract, may not exist yet
- 🧪 **Experimental**  available but may change without full deprecation guarantees

**Rule:** Anything not explicitly marked  is 🛠.

### Promotion criteria (🛠  )
A connector capability becomes  only when:
- the connector interface is implemented,
- profile resolution is enforced,
- and at least one conformance test validates behavior (paging, retries, checkpoints, failure modes).

---

## What a connector is (and is not)

A **Connector** is a controlled ingress boundary between Chartly and an external system.

A connector **IS**:
- a **profile-driven ingestion engine**
- stateless with respect to data (no durable storage beyond checkpoints)
- deterministic given the same inputs
- observable (metrics, logs, audit events)

A connector **IS NOT**:
- a general-purpose script runner
- a place to embed secrets or credentials
- a long-lived data store
- a business-logic engine

---

## Connector mental model

~~~text
External System
      
       HTTP / DB / Queue / File
      

   Connector      profile-driven behavior
   Instance    

         canonical events
        

  Normalizer(s)   

~~~

Connectors ingest **raw external data** and emit **canonical events** for downstream processing.

---

## Connector responsibilities

A connector MUST:
- fetch data using a resolved **connector profile**
- respect timeouts, retries, and rate limits
- enforce egress and SSRF guardrails
- emit events using the canonical event contract
- surface progress, errors, and metrics

A connector MUST NOT:
- bypass profile rules
- embed authentication material
- perform schema transformation beyond transport shaping

---

## Connector types

| Type        | Examples                    | Notes |
|-------------|-----------------------------|-------|
| `http`      | REST / JSON APIs            | Most common; paging + auth |
| `database`  | Postgres, MySQL replicas    | Read-only ingestion |
| `queue`     | Kafka-like systems          | Offset/ack driven |
| `file`      | Object stores, FTP          | Batch-oriented |
| `webhook`   | External push               | Signed inbound payloads |

**Note:** Type determines validation schema and runtime adapter, not deployment topology.

---

## Connector lifecycle

1. **Definition**  
   Profile authored, versioned, reviewed.

2. **Resolution**  
   Profile resolved and **frozen** for the run.

3. **Execution**  
   Connector ingests data, emits events, updates checkpoints.

4. **Completion**  
   Success or failure recorded with final checkpoint.

---

## Event emission contract (binding)

Connectors MUST emit events that conform to the **canonical event contract**.

### Required event fields (minimum)
- `event_id`  unique identifier (stable per emitted record)
- `occurred_at`  RFC3339 UTC timestamp from source
- `ingested_at`  RFC3339 UTC timestamp of ingestion
- `source_ref`  connector + profile reference
- `raw_ref`  pointer or hash to raw payload (if retained)

Events SHOULD also include:
- `tenant_id`
- `project_id`
- `connector_id`
- `sequence` (monotonic per checkpoint scope)

**Rule:** If an emitted event does not satisfy the canonical contract, the connector MUST fail fast.

---

## Checkpoint contract (minimal)

Checkpoints allow connectors to resume deterministically.

### Scope
A checkpoint is scoped to:
- `tenant_id`
- `project_id`
- `connector_id`
- `partition` (optional; e.g., shard, topic, table)

### Shape
- `checkpoint_type` (e.g., cursor, offset, watermark)
- `payload` (opaque bytes / JSON)
- `updated_at` (RFC3339 UTC)

### Semantics
- Checkpoints are opaque to the platform but **stable to the connector**.
- Connectors are **at-least-once** by default.
- Exactly-once behavior MAY be achieved by idempotent downstream handling.

---

## Pagination & iteration

### Supported modes
- `cursor`
- `offset`
- `page`
- `time-window`
- `none`

### Invariants
- No implicit skipping or duplication.
- Cursor advancement MUST be explicit.
- Partial page failures MUST be retryable.

---

## Retry & failure semantics

### Retry classes
| Class        | Description                        |
|--------------|------------------------------------|
| transient    | network errors, upstream 5xx       |
| throttled    | upstream rate limits               |
| permanent    | invalid request/data               |
| auth         | authentication/authorization error |

### Mapping to platform error codes
| Retry class | Platform code |
|------------|---------------|
| transient  | `unavailable` |
| throttled  | `rate_limited` |
| permanent  | `invalid_argument` |
| auth       | `unauthenticated` or `permission_denied` |

### Rules
- Backoff MUST be bounded.
- Jitter is recommended.
- Infinite retries are forbidden.

---

## Rate limiting

Connectors MUST respect:
- remote limits (from profile)
- platform limits (global safety caps)

Rate limit breaches MUST surface:
- error code `rate_limited`
- `Retry-After` when available

---

## Security posture (connectors)

### Hard rules
- No secrets in code or profiles.
- Auth via `secretRef` only.
- Egress restricted by allowlist.
- SSRF guardrails MUST block by default:
  - localhost
  - link-local
  - metadata IP ranges
  - private CIDRs unless explicitly allowed
- TLS validation enabled by default.

---

## Observability

### Metrics
- requests_total
- requests_failed_total
- bytes_ingested_total
- pages_processed_total
- retries_total
- duration_seconds

### Logs
- structured
- correlated
- redacted

### Audit events
- connector.run.started
- connector.run.succeeded
- connector.run.failed
- connector.profile.resolved

---

## Inbound webhooks (special case)

### Rules
- Payloads MUST be schema-validated.
- Signatures REQUIRED.
- Replay protection REQUIRED.
- No side effects before validation.

---

## Example connector profile (no secrets)

~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: connector
  name: http-json
  version: 1.0.0
  description: Generic HTTP JSON connector
  owners: [team:integrations]
spec:
  transport:
    baseUrl: "https://api.example.invalid"
    timeoutMs: 10000
    maxPayloadBytes: 1048576
  egressPolicy:
    dnsAllowlist:
      - "api.example.invalid"
  auth:
    mode: bearer
    secretRef:
      name: connector-http-json
      key: token
  pagination:
    mode: cursor
    requestParam: cursor
    responseField: next_cursor
  retry:
    maxAttempts: 4
    backoffMs: 500
  rateLimit:
    maxRequestsPerMinute: 300
~~~

---

## Operator checklist

Before enabling a connector:
- [ ] Profile validated and reviewed
- [ ] Egress allowlist configured
- [ ] Timeouts, retries, and rate limits set
- [ ] Checkpoint semantics verified
- [ ] Determinism test passes
- [ ] Metrics and audit events visible
- [ ] Rollback plan identified

---

## Next steps (🛠)

- Define a minimal connector SDK/interface
- Add conformance tests for:
  - pagination correctness
  - retry classification
  - checkpoint determinism
  - SSRF protection
- Implement one reference connector end-to-end
