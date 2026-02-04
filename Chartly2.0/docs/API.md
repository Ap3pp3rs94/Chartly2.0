# Chartly 2.0  API

## Contract status & trust model

This document is a **contract scaffold**: it describes the intended public API surface and rules so implementations stay consistent.

### Legend
-  **Implemented**  verified in code and/or conformance tests
- 🛠 **Planned**  desired contract, may not exist yet
- 🧪 **Experimental**  available but may change without full deprecation guarantees

**Rule:** If an endpoint/behavior is not explicitly marked , treat it as 🛠.

### Promotion criteria (🛠  )
Mark an item  only when:
- the endpoint exists, and
- the response shape matches this contract, and
- a minimal conformance test asserts the behavior.

## Purpose

Chartly exposes a **provider-neutral, automation-first API** for operating data workflows end-to-end: define connectors, ingest/normalize data, run analytics, observe execution, and retrieve audit history. The same API should serve UI, CLI, and CI/CD automation.

## Design goals

- **Single public surface** through a Gateway, with clear internal service boundaries
- **Contract-first & versioned** (OpenAPI recommended) so automation can be long-lived
- **Safe-by-default** semantics: idempotency, optimistic concurrency, least privilege
- **Operationally excellent**: health/readiness, correlation IDs, predictable error shapes
- **Provider-neutral**: no dependency on a specific cloud, service mesh, or identity vendor

## Non-goals

- Exposing infrastructure/provider identifiers (resource IDs remain Chartly-opaque)
- Forcing one ingress/controller or one identity provider
- Freezing internal service-to-service protocols as part of the public contract

## API surface & routing

External clients interact with a single entrypoint (Gateway). The Gateway routes by resource domain to internal services.

### Base path
- 🛠 `/v1` (major version)

### Domain routing map (Gateway  service)
- 🛠 `/v1/auth/**`  `chartly-auth`
- 🛠 `/v1/workflows/**`, `/v1/runs/**`, `/v1/operations/**`  `chartly-orchestrator`
- 🛠 `/v1/connectors/**`  `chartly-connector-hub`
- 🛠 `/v1/normalize/**` (optional admin/debug)  `chartly-normalizer`
- 🛠 `/v1/datasets/**`  `chartly-storage`
- 🛠 `/v1/analytics/**`  `chartly-analytics`
- 🛠 `/v1/audit/**`  `chartly-audit`
- 🛠 `/v1/system/**`  `chartly-observer`

## Versioning & compatibility

- Major versions use a path prefix: `/v1`, `/v2`, 
- Within a major version, changes are **backward compatible** (additive fields/endpoints; no breaking removals)
- Breaking changes require a new major version and a deprecation window

## Content types & encoding

- Requests: `application/json; charset=utf-8`
- Responses: `application/json; charset=utf-8`
- Optional streaming:
  - SSE: `text/event-stream`
  - NDJSON: `application/x-ndjson`
- Compression MAY be enabled (e.g., gzip); clients SHOULD accept compressed responses

## Authentication & authorization

Chartly remains provider-neutral and integrates with standards-compliant identity systems.

- Authentication: Bearer tokens (e.g., OAuth2/OIDC JWTs) and/or session cookies for UI flows
- Authorization: RBAC enforced at the Gateway and within services (defense-in-depth)
- Tenancy: requests resolve tenant/project context via token claims and/or explicit project routing

### Illustrative permission scopes
- `workflows:read`, `workflows:write`, `workflows:run`
- `connectors:read`, `connectors:write`
- `datasets:read`, `datasets:write`
- `analytics:read`, `analytics:write`
- `audit:read`
- `system:read`

## Request metadata, correlation, and retries

Clients SHOULD provide a **request ID**; if missing, Chartly generates one.

### Request headers
- `Authorization: Bearer <token>`
- `Content-Type: application/json`
- `Accept: application/json`
- `X-Request-Id: <client-request-id>` (optional)
- `X-Idempotency-Key: <opaque-key>` (optional, recommended for create/mutate)

### Response headers
- `X-Request-Id: <request-id>`
- `ETag: "<version-tag>"` (when applicable)
- Rate limiting (when enabled):
  - `RateLimit-Limit`, `RateLimit-Remaining`, `RateLimit-Reset`
  - `Retry-After` (especially on `429`)

## Default size limits (provider-neutral, illustrative)

These defaults exist to keep the API operable under load. Environments MAY tune them, but behavior should remain consistent.

- Max request body (typical JSON endpoints): **110 MB**
- Ingestion-style endpoints beyond that: **streaming or chunking recommended**
- Max header size: **816 KB**
- Max URL length: **48 KB**
- Max response size: prefer **pagination**; for large results use **streaming** (SSE/NDJSON) or asynchronous operations

### Limit enforcement behavior
- Request body too large: return **413** with error code `payload_too_large`
- Headers too large (if enforced explicitly): return **431** with error code `header_too_large`

## Resource model conventions

- `id` fields are **opaque strings** (implementation may be UUID/ULID); treat as case-sensitive
- Timestamps are RFC3339 UTC (e.g., `2026-02-02T18:04:05Z`)
- Common metadata:
  - `labels`: small indexed hints
  - `annotations`: freeform metadata

## Standard response envelopes

Chartly uses a consistent envelope for predictable clients.

### Success (single resource)
~~~json
{"data":{"id":"res_123","name":"example","created_at":"2026-02-02T18:04:05Z","updated_at":"2026-02-02T18:04:05Z"},"meta":{"request_id":"req_abc"}}
~~~

### Success (list + cursor pagination)
~~~json
{"data":[{"id":"res_1"},{"id":"res_2"}],"page":{"next_cursor":"cursor_123"},"meta":{"request_id":"req_abc"}}
~~~

### Error (stable shape)
~~~json
{"error":{"code":"invalid_argument","message":"Field 'name' is required.","details":[{"field":"name","location":"body","reason":"required","message":"missing required field"}]},"meta":{"request_id":"req_abc"}}
~~~

## Error contract

### HTTP status mapping
- `400`  `invalid_argument`, `failed_precondition`
- `401`  `unauthenticated`
- `403`  `permission_denied`
- `404`  `not_found`
- `409`  `conflict`, `already_exists`
- `412`  `precondition_failed` (ETag / `If-Match` mismatch)
- `413`  `payload_too_large`
- `429`  `rate_limited` (include `Retry-After`)
- `431`  `header_too_large` (if enforced)
- `500`  `internal`
- `503`  `unavailable`

### Error code selection rules (strict)
- `invalid_argument` (400): request JSON is valid but semantically invalid (missing required fields, bad formats, invalid enum values).
- `failed_precondition` (400): request is valid but cannot be performed due to current system/resource state (e.g., cannot cancel a completed run).
- `already_exists` (409): create would duplicate an existing resource using a client-controlled unique key (e.g., name within a project if name is enforced-unique).
- `conflict` (409): request conflicts with current state/version not covered by `If-Match` (e.g., concurrent state transition) or violates uniqueness not directly tied to a client-controlled key.
- `precondition_failed` (412): ONLY for optimistic concurrency failures (`If-Match` vs `ETag`).
- `payload_too_large` (413): request body exceeds configured size limit.
- `header_too_large` (431): headers exceed configured size limit.

### `details[]` schema (canonical)
Each entry in `error.details[]` SHOULD be an object:
- `field` (string): field name (e.g., `name`, `limit`, `workflow_id`)
- `location` (string): `body|query|path|header`
- `reason` (string): machine-readable reason (e.g., `required|invalid|out_of_range|unknown|too_large`)
- `message` (string, optional): human-oriented hint (short)

## Pagination, filtering, sorting

### Cursor pagination
List endpoints support:
- `limit` (default 50, max 200)
- `cursor` (opaque, from `page.next_cursor`)

Example: `GET /v1/workflows?limit=50&cursor=cursor_123`

### Filtering and sorting
- `filter`: field predicates (documented per endpoint in OpenAPI/contract)
- `sort`: comma-separated fields, prefix `-` for descending (e.g., `sort=-created_at,name`)

## Idempotency & concurrency control

### Idempotency (recommended)
- Create/mutate endpoints accept `X-Idempotency-Key` so retries dont duplicate work.
- Idempotency is scoped to: authenticated principal + endpoint + key.

### Optimistic concurrency (ETag)
- Resources MAY return `ETag`.
- Clients MAY send `If-Match: "<etag>"` on updates to avoid lost updates.
- `412 precondition_failed` MUST be used when `If-Match` fails.

## Long-running operations (async pattern)

Some requests are asynchronous (large connector syncs, heavy analytics queries).

Pattern:
1. Client triggers work (POST)
2. Server returns `202 Accepted` with an operation resource
3. Client polls `GET /v1/operations/{operation_id}` until a terminal state

### Operation object (typical fields)
- `id`
- `state`: `pending|running|succeeded|failed|canceled`
- `started_at`, `finished_at`
- `result` (optional), `error` (optional)

### Operation state machine (contract)
| From      | To                          | Trigger |
|----------|------------------------------|---------|
| pending  | running                       | worker starts |
| pending  | canceled                      | client cancels before start |
| running  | succeeded                     | work completes successfully |
| running  | failed                        | work fails (terminal) |
| running  | canceled                      | client/admin cancel |
| *terminal* | *(no transitions)*          |  |

Terminal states: `succeeded`, `failed`, `canceled`.

## Workflow spec (conceptual contract)

The examples reference `workflow.spec`. This section defines the conceptual shape so examples are not ghost contracts.

### Workflow resource (high level)
- `id`, `name`, `labels`, `annotations`
- `spec`: the execution plan
- `status` (optional): summary fields (e.g., last run)

### `spec` (conceptual)
~~~json
{"steps":[{"type":"connector_sync","connector_id":"con_123"},{"type":"normalize","profile":"default"},{"type":"analytics_query","query_ref":"qry_daily"}]}
~~~

### Step types (illustrative, extensible)
- `connector_sync`
  - required: `connector_id`
- `normalize`
  - required: `profile` (normalization profile name/reference)
- `analytics_query`
  - required: `query_ref` (reference to a saved query or query template)
- `write_dataset` (optional)
  - required: `dataset_id` (target dataset)

### Common step fields (optional)
- `name` (string): step label
- `timeout_ms` (integer)
- `retry` (object): `{ "max_attempts": int, "backoff_ms": int }`
- `when` (object): conditional execution rules

## Events & webhooks

Chartly can emit events for integration and automation.

### Delivery options
- SSE for near-real-time UI/CLI streaming
- Webhooks for external systems (recommended for automation)
- Polling is always available via runs/operations endpoints

### Illustrative event types
- `workflow.run.started`, `workflow.run.succeeded`, `workflow.run.failed`
- `connector.sync.started`, `connector.sync.succeeded`, `connector.sync.failed`
- `dataset.updated`, `dataset.version.created`
- `audit.event.written`

### Webhook request headers (format only; no secrets)
When webhooks are enabled, Chartly SHOULD send:
- `X-Chartly-Event-Id: <opaque-event-id>`
- `X-Chartly-Timestamp: <unix-seconds>`
- `X-Chartly-Signature: v1=<hex>` (signature over timestamp + body)

Signature guidance (provider-neutral):
- Algorithm: HMAC-SHA256
- Signed payload format: `<timestamp>.<raw_body>`
- Rotation: support multiple active secrets (verify against newest and previous during rotation)
- Replay defense: reject timestamps outside an allowed skew window

## Observability hooks (API-facing)

- Health/readiness endpoints for probes
- `X-Request-Id` propagation across service calls
- Per-service Prometheus metrics endpoints (commonly `/metrics`) protected by network controls appropriate to the environment

## Endpoint overview (v1)

**Note:** Status defaults to 🛠 until verified.

### Health / readiness (probe contract)
- 🛠 `GET /health`
- 🛠 `GET /ready`

**Probe rule:** `/health` and `/ready` MUST be reachable **without authentication** and MUST return `200` when healthy. These endpoints are intended for liveness/readiness checks.

Versioned equivalents MAY exist:
- 🛠 `GET /v1/health`
- 🛠 `GET /v1/ready`
- 🛠 `GET /v1/version`

If `/v1/health` and `/v1/ready` exist, they SHOULD follow the same unauthenticated probe behavior unless an environment explicitly forbids it. Probe availability MUST NOT depend on identity provider uptime.

### Auth
- 🛠 `POST /v1/auth/session` (optional UI/session flow)
- 🛠 `GET /v1/auth/me`

### Workflows (orchestrator)
- 🛠 `GET /v1/workflows`
- 🛠 `POST /v1/workflows`
- 🛠 `GET /v1/workflows/{workflow_id}`
- 🛠 `PATCH /v1/workflows/{workflow_id}`
- 🛠 `DELETE /v1/workflows/{workflow_id}`
- 🛠 `POST /v1/workflows/{workflow_id}:run`

### Runs (orchestrator)
- 🛠 `GET /v1/runs`
- 🛠 `GET /v1/runs/{run_id}`
- 🛠 `POST /v1/runs/{run_id}:cancel`

### Operations (async)
- 🛠 `GET /v1/operations/{operation_id}`

### Connectors (connector hub)
- 🛠 `GET /v1/connectors`
- 🛠 `POST /v1/connectors`
- 🛠 `GET /v1/connectors/{connector_id}`
- 🛠 `PATCH /v1/connectors/{connector_id}`
- 🛠 `POST /v1/connectors/{connector_id}:sync`

### Datasets (storage)
- 🛠 `GET /v1/datasets`
- 🛠 `POST /v1/datasets`
- 🛠 `GET /v1/datasets/{dataset_id}`
- 🛠 `GET /v1/datasets/{dataset_id}/versions`
- 🛠 `POST /v1/datasets/{dataset_id}/ingestions`

### Analytics
- 🛠 `POST /v1/analytics/queries` (sync or async)
- 🛠 `GET /v1/analytics/queries/{query_id}`

### Audit
- 🛠 `GET /v1/audit/events`
- 🛠 `GET /v1/audit/events/{event_id}`

### Observer / system
- 🛠 `GET /v1/system/status`
- 🛠 `GET /v1/system/components`

## Examples (no secrets)

### List workflows
~~~bash
curl -sS -H "Authorization: Bearer $TOKEN" -H "Accept: application/json" "https://chartly.example/v1/workflows?limit=50"
~~~

### Create a workflow (idempotent)
~~~bash
curl -sS -X POST "https://chartly.example/v1/workflows" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -H "X-Idempotency-Key: 8b2d2c9b-3f9d-4a5b-a1a0-000000000001" -d '{"name":"daily-rollup","labels":{"team":"data"},"spec":{"steps":[{"type":"connector_sync","connector_id":"con_123"},{"type":"normalize","profile":"default"},{"type":"analytics_query","query_ref":"qry_daily"}]}}'
~~~

### Start a run (async-friendly)
~~~bash
curl -sS -X POST "https://chartly.example/v1/workflows/wf_123:run" -H "Authorization: Bearer $TOKEN" -H "Accept: application/json"
~~~

## OpenAPI contract (recommended)

- 🛠 The public API SHOULD be defined by an OpenAPI document (e.g., `openapi.yaml`).
- 🛠 CI SHOULD validate:
  - spec validity
  - breaking changes detection
  - examples conforming to schemas
- 🛠 Gateway routing SHOULD be kept consistent with the OpenAPI surface (via tests or generation).

## Security baseline (API-facing)

- TLS required for public endpoints; plaintext HTTP is not acceptable
- Strict validation and size limits for headers and bodies
- Rate limiting at the Gateway; return `Retry-After` on `429`
- CORS disabled by default; enable only for known UI origins
- Structured logging with redaction; never log sensitive fields
- Audit events for security-relevant actions (auth changes, workflow/run triggers, connector writes)
