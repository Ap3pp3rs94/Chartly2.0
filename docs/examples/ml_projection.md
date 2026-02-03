# ML Projection (Example Playbook)

## Status & intent

- status: 🛠 roadmap (example playbook)
- scope: single-tenant, non-production, bounded dataset
- goal: demonstrate a **safe ML projection pattern**: deterministic feature extraction  deterministic inference  persisted projection
- safety: inference-only; no training; no uncontrolled model updates; deterministic windows; auditable freezing

If a conformance test and this example conflict, **the test is authoritative** and this example MUST be updated.

---

## What ML projection means in Chartly

An **ML projection** is a derived dataset produced by applying a **fixed model artifact** to canonical data.

Key properties:
- inference-only (no training)
- model version pinned (immutable)
- deterministic feature extraction (explicit window bounds)
- repeatable outputs for identical inputs
- auditable (model ref + resolved digest + profile hashes + output hash)

This playbook uses a fictional projection: **`sales-demand`**.

---

## Security and determinism constraints (binding)

- **No secrets** in profiles or workflows
- **No outbound calls to systems outside Chartlys data plane** during inference
- **Pinned model versions** only (no `latest`)
- **Explicit window bounds** only (no relative now)
- **Bounded compute** (batch size, timeout, resource caps)
- **Audit evidence required** (missing audit = incomplete)

---

## Prerequisites (explicit)

You must have:
- a running Chartly environment (`dev` recommended)
- tenant/project context
- a canonical dataset populated (e.g., `sales-events`)
- permissions: read datasets, run analytics, write derived datasets
- a model artifact available to the platform (packaged and addressable)

If these are not true, stop and fix them first.

---

## Minimal artifacts created

1. A **feature extraction profile** (analytics)
2. A **projection profile** (inference contract) *(schema may be roadmap)*
3. A **storage policy profile** (projection dataset)
4. A workflow that produces the projection
5. Fixtures + golden outputs for determinism

---

## Step 1  Define scope, naming, and fixed window

### Conventions
- input dataset: `sales-events`
- feature dataset: `sales-demand-features`
- projection dataset: `sales-demand-projection`
- fixed test window (explicit):
  - `window_start`: `2026-02-01T00:00:00Z`
  - `window_end`: `2026-02-08T00:00:00Z`

### Determinism rule (binding)
Given the same:
- input dataset contents within `[window_start, window_end)`
- feature profile version
- projection profile version
- model ref (immutable artifact)

the projection MUST produce identical outputs.

---

## Step 2  Feature extraction profile (analytics)

Create `profiles/analytics-sales-demand-features@1.0.0.yaml`:

~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: analytics
  name: sales-demand-features
  version: 1.0.0
  description: Deterministic feature extraction for sales demand projection
  owners: [team:analytics]
  labels: {tier: roadmap}
spec:
  inputs:
    dataset: sales-events
  outputs:
    dataset: sales-demand-features
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
      ORDER BY 1,2
~~~

**Rules**
- Feature queries MUST use explicit window bounds.
- Output ordering MUST be stable for golden comparisons (`ORDER BY 1,2`).
- Timezone semantics MUST be documented and consistent (UTC for this example).

---

## Step 3  Projection profile (inference contract)

### Status choice (contract honesty)

This example uses a `projection` profile kind. If the profile schema is not available yet, treat this section as 🛠 roadmap and do not mark it .

Create `profiles/projection-sales-demand@1.0.0.yaml`:

~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: projection
  name: sales-demand
  version: 1.0.0
  description: Inference-only projection using a pinned model artifact (identity baseline model)
  owners: [team:ml-platform]
  labels: {tier: roadmap}
spec:
  mode: inference
  input:
    dataset: sales-demand-features
    key_fields: ["day","currency"]
  output:
    dataset: sales-demand-projection
    key_fields: ["day","currency"]
  model:
    ref: "model://sales-demand@1.0.0"
    format: "onnx"
  runtime:
    maxBatchSize: 256
    timeoutMs: 30000
    maxCpuCores: 2
    maxMemoryMb: 512
  safety:
    allowExternalNetwork: false
    allowFileWrites: false
~~~

**Contract rules**
- `metadata.kind: projection` is required for schema correctness (analytics profiles MUST NOT carry inference fields).
- `model.ref` MUST resolve to an immutable artifact (resolved to a digest internally).
- Inference MUST be pure: read features dataset  write projection dataset.
- No dynamic downloads during execution.
- If the model cannot be resolved, the run MUST fail fast.

### Deterministic baseline model note (golden outputs)
This example assumes `model://sales-demand@1.0.0` is a deterministic **identity baseline model** packaged specifically for regression testing:
- output fields are deterministic functions of input features
- no randomness
- no external dependencies

---

## Step 4  Storage profile for projection dataset

Create `profiles/storage-sales-demand-projection@1.0.0.yaml`:

~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: storage
  name: sales-demand-projection
  version: 1.0.0
  description: Storage policy for ML projections (sales demand)
  owners: [team:data-platform]
  labels: {tier: roadmap}
spec:
  dataset: sales-demand-projection
  retentionDays: 30
  partitioning:
    by: ["day"]
    granularity: day
  indexing:
    fields: ["day", "currency"]
~~~

---

## Step 5  Workflow: features  inference (materializes output)

This playbook commits to **Pattern A**:

- feature step materializes `sales-demand-features`
- inference step reads `sales-demand-features` and **materializes** `sales-demand-projection`
- no separate write step is required

Create `workflows/sales-demand-projection@1.0.0.yaml`:

~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Workflow
metadata:
  name: sales-demand-projection
  version: 1.0.0
  labels:
    domain: sales
    kind: ml-projection
spec:
  params:
    window_start: "2026-02-01T00:00:00Z"
    window_end: "2026-02-08T00:00:00Z"
  steps:
    - type: analytics_query
      analytics_profile: "tenant-demo/proj-sales::analytics/sales-demand-features@1.0.0"
      params:
        window_start: "{{params.window_start}}"
        window_end: "{{params.window_end}}"
    - type: projection_infer
      projection_profile: "tenant-demo/proj-sales::projection/sales-demand@1.0.0"
      params:
        window_start: "{{params.window_start}}"
        window_end: "{{params.window_end}}"
~~~

### Schema note
Workflow YAML uses explicit `*_profile` fields for readability. API payloads may use compact fields (see `API.md`). Both MUST resolve through the same profile resolver and freeze resolved hashes.

### Freezing rule (binding)
For the run, Chartly MUST record and freeze:
- feature profile ref + resolved hash
- projection profile ref + resolved hash
- model ref + resolved digest
- storage policy ref + resolved hash (if applied by the writer)

---

## Step 6  Fixtures + determinism validation

### Fixture input (bounded)
Create `fixtures/sales-events.fixture.ndjson`:

~~~text
{"event_id":"sale_a","occurred_at":"2026-02-01T10:00:00Z","amount":10.00,"currency":"USD","customer_id":"c1","type":"sales.event"}
{"event_id":"sale_b","occurred_at":"2026-02-02T11:00:00Z","amount":20.00,"currency":"USD","customer_id":"c2","type":"sales.event"}
{"event_id":"sale_c","occurred_at":"2026-02-03T12:00:00Z","amount":15.00,"currency":"USD","customer_id":"c3","type":"sales.event"}
~~~

### Expected features (golden, semantic rows)
Create `fixtures/features.expected.ndjson`:

~~~text
{"day":"2026-02-01","currency":"USD","orders":1,"total_amount":10.0}
{"day":"2026-02-02","currency":"USD","orders":1,"total_amount":20.0}
{"day":"2026-02-03","currency":"USD","orders":1,"total_amount":15.0}
~~~

### Expected projections (golden, semantic rows)
Create `fixtures/projection.expected.ndjson`:

~~~text
{"day":"2026-02-01","currency":"USD","predicted_orders":1,"predicted_total_amount":10.0,"model_ref":"model://sales-demand@1.0.0"}
{"day":"2026-02-02","currency":"USD","predicted_orders":1,"predicted_total_amount":20.0,"model_ref":"model://sales-demand@1.0.0"}
{"day":"2026-02-03","currency":"USD","predicted_orders":1,"predicted_total_amount":15.0,"model_ref":"model://sales-demand@1.0.0"}
~~~

**Determinism assertions**
- Resolved profile hashes for:
  - `sales-demand-features@1.0.0`
  - `projection/sales-demand@1.0.0`
  - `storage/sales-demand-projection@1.0.0`
  MUST remain identical across runs.
- Feature output MUST match the golden file exactly (stable ordering enforced).
- Projection output MUST match the golden file exactly for the pinned identity baseline model artifact.

---

## Step 7  Run via API (illustrative)

### Start run
~~~bash
curl -sS -X POST "https://chartly.example/v1/workflows/wf_sales_demand_projection:run" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/json"
~~~

---

## Observability & audit (required)

### Metrics (conceptual)
- analytics_queries_total increases
- analytics_duration_seconds recorded
- projection_inference_total increases
- projection_inference_failed_total remains bounded

### Audit events (conceptual)
- analytics.profile.resolved (feature profile ref + hash)
- projection.profile.resolved (projection profile ref + hash)
- ml.model.resolved (model ref + resolved digest)
- workflow.run.started / succeeded
- dataset.updated (features)
- dataset.updated (projection)

If audit evidence is missing, this example is incomplete.

---

## Safety & teardown

This example is non-destructive by default, but it creates derived datasets.

### Teardown order (safe)
1. stop the workflow
2. remove or archive derived datasets (features, projection)
3. deprecate profiles (do not delete versions used by audit evidence)
4. preserve audit logs

---

## Success criteria

This ML projection is complete when:
- profiles validate and are versioned (or projection schema is explicitly marked 🛠 until implemented)
- model ref is pinned and immutable (digest-resolved)
- fixtures produce golden feature output
- inference produces golden projection output
- audit evidence records model ref + resolved digest + profile hashes
- repeated runs yield identical results

---

## Next steps (🛠)

- Implement `projection` profile kind schema and validator
- Implement `projection_infer` step contract
- Add a conformance test that:
  - loads fixtures
  - runs feature query
  - runs inference using pinned identity baseline model
  - compares outputs to golden files
- Promote this example to  canonical after validation
