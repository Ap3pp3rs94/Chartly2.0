# Onboard a New Data Domain (Example Playbook)

## Status & intent

- status: 🛠 roadmap (example playbook)
- scope: single-tenant, non-production, bounded dataset
- goal: demonstrate the **minimum, repeatable path** to onboard a new domain into Chartly
- safety: non-destructive by default; fails explicitly if prerequisites are missing

If a conformance test and this example conflict, **the test is authoritative** and this example MUST be updated.

---

## What new domain means

A **domain** is a coherent slice of data that gets:
- a connector ingestion pattern
- a normalizer mapping to canonical events
- a storage dataset policy
- optional analytics (rollups/queries)
- dashboards/observability hooks

Example domains:
- `sales`
- `inventory`
- `tickets`
- `device_health`

This playbook uses a fictional domain: **`sales`**.

---

## Prerequisites (explicit)

You must have:
- an environment (`dev` recommended)
- a namespace/project context (tenant + project)
- the Chartly services deployed (Gateway, Orchestrator, Connector Hub, Normalizer, Storage, Analytics, Audit, Auth, Observer)
- a method to apply profile files (GitOps sync or mounted profiles)

If any prerequisite is missing, stop and fix it before continuing.

---

## The minimal artifacts you will create

1. A **connector profile** (how to ingest)
2. A **normalizer profile** (how to shape)
3. A **storage profile** (how to retain/partition)
4. An optional **analytics profile** (how to compute)
5. A **workflow** that stitches them together
6. A small **fixture payload** to validate determinism

All artifacts are versioned and reviewable.

---

## Step 1  Choose naming & scoping

### Namespace rule (from PROFILES.md)

Profiles resolve within a namespace:
- `tenant/<tenant_id>/project/<project_id>` (preferred)
- fallback: `global`

This example uses:
- tenant: `tenant-demo`
- project: `proj-sales`

### Domain identifiers (conventions)
- domain slug: `sales`
- canonical event type: `sales.event`
- datasets:
  - raw: `sales-raw` (optional)
  - canonical: `sales-events`
  - derived: `sales-rollup-daily` (optional)

---

## Step 2  Define the connector profile (ingestion)

Create `profiles/connector-sales-http@1.0.0.yaml` (example content):

~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: connector
  name: sales-http
  version: 1.0.0
  description: Ingest sales records from an HTTP JSON API (bounded example)
  owners: [team:integrations]
  labels: {tier: roadmap}
spec:
  transport:
    baseUrl: "https://api.example.invalid"
    timeoutMs: 10000
    maxPayloadBytes: 1048576
    headers:
      Accept: "application/json"
  egressPolicy:
    dnsAllowlist:
      - "api.example.invalid"
    blockCidrs:
      - "127.0.0.0/8"
      - "169.254.0.0/16"
  auth:
    mode: bearer
    secretRef:
      name: sales-http
      key: token
  pagination:
    mode: cursor
    requestParam: cursor
    responseField: next_cursor
  retry:
    maxAttempts: 3
    backoffMs: 500
  rateLimit:
    maxRequestsPerMinute: 120
~~~

**Notes**
- `secretRef` points to credentials, but contains no secret values.
- Egress allowlist + SSRF blocks are required guardrails.

---

## Step 3  Define the normalizer profile (canonicalization)

Create `profiles/normalizer-sales-default@1.0.0.yaml`:

~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: normalizer
  name: sales-default
  version: 1.0.0
  description: Map inbound sales payloads into canonical sales events
  owners: [team:data-platform]
  labels: {tier: roadmap}
spec:
  canonicalEventType: sales.event
  requiredFields:
    - event_id
    - occurred_at
    - amount
    - currency
  mapping:
    event_id: "$.id"
    occurred_at: "$.timestamp"
    amount: "$.totals.amount"
    currency: "$.totals.currency"
    customer_id: "$.customer.id"
  transforms:
    - op: toNumber
      field: amount
    - op: toUtcTimestamp
      field: occurred_at
~~~

**Rule**: The normalizer MUST reject events missing required fields.

---

## Step 4  Define the storage profile (retention/partition)

Create `profiles/storage-sales-events@1.0.0.yaml`:

~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: storage
  name: sales-events
  version: 1.0.0
  description: Storage policy for canonical sales events
  owners: [team:data-platform]
  labels: {tier: roadmap}
spec:
  dataset: sales-events
  retentionDays: 30
  partitioning:
    by: ["occurred_at"]
    granularity: day
  indexing:
    fields: ["event_id", "customer_id", "currency"]
~~~

**Rule**: Storage growth for this dataset must not degrade unrelated tenants/projects.

---

## Step 5  Optional: define an analytics profile (derived rollup)

Create `profiles/analytics-sales-daily-rollup@1.0.0.yaml`:

~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: analytics
  name: sales-daily-rollup
  version: 1.0.0
  description: Daily sales totals rollup
  owners: [team:analytics]
  labels: {tier: roadmap}
spec:
  inputs:
    dataset: sales-events
    window: P1D
  outputs:
    dataset: sales-rollup-daily
  queryTemplate:
    dialect: "sql"
    text: |
      SELECT
        date_trunc('day', occurred_at) AS day,
        currency,
        count(*) AS orders,
        sum(amount) AS total_amount
      FROM {{dataset}}
      WHERE occurred_at >= {{window_start}} AND occurred_at < {{window_end}}
      GROUP BY 1,2
~~~

---

## Step 6  Define a workflow that stitches profiles together

Create `workflows/sales-onboard@1.0.0.yaml`:

~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Workflow
metadata:
  name: sales-onboard
  version: 1.0.0
  labels:
    domain: sales
spec:
  steps:
    - type: connector_sync
      connector_profile: "tenant-demo/proj-sales::connector/sales-http@1.0.0"
    - type: normalize
      normalizer_profile: "tenant-demo/proj-sales::normalizer/sales-default@1.0.0"
    - type: write_dataset
      storage_profile: "tenant-demo/proj-sales::storage/sales-events@1.0.0"
    - type: analytics_query
      analytics_profile: "tenant-demo/proj-sales::analytics/sales-daily-rollup@1.0.0"
      when:
        enabled: true
~~~

### Schema note (important)
Workflow YAML uses explicit `*_profile` fields for readability and linting.  
API payloads MAY use more compact fields (as defined by `API.md`). Both forms MUST resolve to the same internal contract.

**Rules**
- No `@latest`.
- Profiles are explicit and versioned.
- The resolved profiles MUST be frozen for the run.

---

## Step 7  Validate with a bounded fixture

Create `fixtures/sales.sample.json` (small, deterministic input):

~~~json
{
  "id": "sale_001",
  "timestamp": "2026-02-02T12:00:00Z",
  "totals": { "amount": "19.99", "currency": "USD" },
  "customer": { "id": "cust_123" }
}
~~~

### Expected canonical output (semantic payload)
Create `fixtures/sales.expected.canonical.json`:

~~~json
{
  "event_id": "sale_001",
  "occurred_at": "2026-02-02T12:00:00Z",
  "amount": 19.99,
  "currency": "USD",
  "customer_id": "cust_123",
  "type": "sales.event"
}
~~~

### Envelope note (important)
The expected output above shows the **semantic payload** only.  
Production canonical events include additional envelope metadata (e.g., `tenant_id`, `project_id`, `ingested_at`, `raw_ref`) as defined by the canonical event contract.

**Determinism rule**
- Same fixture + same profiles  identical canonical output and identical resolved profile hashes.

---

## Step 8  Run the workflow (API-level example)

This is illustrative (status 🛠). Real endpoints and schemas must match `API.md`.

### Create workflow (idempotent)
~~~bash
curl -sS -X POST "https://chartly.example/v1/workflows" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: 00000000-0000-0000-0000-000000000542" \
  -d '{"name":"sales-onboard","spec":{"steps":[{"type":"connector_sync","connector_id":"tenant-demo/proj-sales::connector/sales-http@1.0.0"},{"type":"normalize","profile":"tenant-demo/proj-sales::normalizer/sales-default@1.0.0"},{"type":"write_dataset","dataset_id":"tenant-demo/proj-sales::storage/sales-events@1.0.0"},{"type":"analytics_query","query_ref":"tenant-demo/proj-sales::analytics/sales-daily-rollup@1.0.0"}]}}'
~~~

### Start run
~~~bash
curl -sS -X POST "https://chartly.example/v1/workflows/wf_123:run" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/json"
~~~

---

## Step 9  Confirm observability & audit

During the run, confirm:

### Metrics (conceptual)
- connector requests_total increases
- bytes_ingested_total increases
- retries_total remains bounded
- duration_seconds recorded

### Audit events (conceptual)
- connector.profile.resolved
- connector.run.started / succeeded
- workflow.run.started / succeeded
- dataset.updated
- analytics.query.executed (optional)

If audit evidence is missing, the onboarding is incomplete.

---

## Safety & teardown

This example is non-destructive by default, but if you created resources you must be able to remove them safely.

### Teardown order (safe)
Teardown SHOULD occur in reverse dependency order:
1. workflows
2. analytics outputs (derived datasets)
3. canonical datasets (if created)
4. profiles (or mark deprecated)
5. preserve audit logs (never delete as cleanup)

### Teardown expectations
- Delete the workflow definition
- Remove profiles (or mark deprecated)
- Remove derived datasets (if created)
- Preserve audit logs

---

## Success criteria (definition of done)

A domain is onboarded when:
- profiles exist, are versioned, and validated
- ingestion runs deterministically on fixtures
- canonical events match expectations
- storage policy is enforced
- analytics (if present) produces repeatable output
- observability and audit evidence exist

---

## Next steps (🛠)

- Add a conformance test that:
  - resolves all profiles
  - hashes resolved specs
  - compares fixture canonical output to golden files
- Promote this example to  canonical once validated end-to-end
