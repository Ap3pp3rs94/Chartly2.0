# Build a Custom Report (Example Playbook)

## Status & intent

- status: 🛠 roadmap (example playbook)
- scope: single-tenant, non-production, bounded dataset
- goal: demonstrate how to create a **custom analytics report** on top of existing canonical data
- safety: read-only by default; fails explicitly if prerequisites are missing

If a conformance test and this example conflict, **the test is authoritative** and this example MUST be updated.

---

## What custom report means in Chartly

A **custom report** is a derived, read-only view of data that:
- reads from one or more canonical datasets
- applies deterministic analytics logic
- produces repeatable output (dataset or query result)
- does **not** mutate source data

Reports are implemented via **analytics profiles** and exposed via the API.

---

## Prerequisites (explicit)

You must have:
- a running Chartly environment (`dev` recommended)
- a tenant/project context
- a canonical dataset populated (e.g., `sales-events`)
- permission to read datasets and run analytics queries

If these are not true, stop and fix them first.

---

## Minimal artifacts created

1. An **analytics profile** (report definition)
2. An optional **derived dataset** (materialized report)
3. An API query example to retrieve results
4. A small fixture to validate determinism

No connectors or storage profiles are created in this example.

---

## Step 1  Define report scope & determinism boundaries

### Conventions
- report slug: `sales-by-customer`
- source dataset: `sales-events`
- output dataset (optional): `sales-by-customer-daily`
- window granularity: day (explicit start/end)

### Determinism rule (binding)
Given the same:
- input dataset contents
- explicit `window_start` and `window_end`
- analytics profile ref + resolved profile hash

the report MUST produce identical results.

**Rule:** For determinism, do not use relative now windows. Use explicit time bounds.

---

## Step 2  Create the analytics profile (report definition)

Create `profiles/analytics-sales-by-customer@1.0.0.yaml`:

~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: analytics
  name: sales-by-customer
  version: 1.0.0
  description: Daily sales totals grouped by customer
  owners: [team:analytics]
  labels: {tier: roadmap}
spec:
  inputs:
    dataset: sales-events
  outputs:
    dataset: sales-by-customer-daily
  queryTemplate:
    dialect: "sql"
    text: |
      SELECT
        date_trunc('day', occurred_at) AS day,
        customer_id,
        count(*) AS orders,
        sum(amount) AS total_amount
      FROM {{dataset}}
      WHERE occurred_at >= {{window_start}} AND occurred_at < {{window_end}}
      GROUP BY 1,2
~~~

**Rules**
- The profile is versioned and immutable once published.
- No secrets or environment-specific values are allowed.
- Window bounds MUST be supplied explicitly at execution time.

---

## Step 3  Validate with a bounded fixture

This example assumes the dataset `sales-events` contains only the bounded fixture records for the test window.

Create `fixtures/sales-events.fixture.ndjson`:

~~~text
{"event_id":"sale_002","occurred_at":"2026-02-03T09:30:00Z","amount":42.50,"currency":"USD","customer_id":"cust_456","type":"sales.event"}
~~~

### Fixed test window (explicit)
- `window_start`: `2026-02-03T00:00:00Z`
- `window_end`: `2026-02-04T00:00:00Z`

### Expected report output (semantic row)
~~~json
{
  "day": "2026-02-03",
  "customer_id": "cust_456",
  "orders": 1,
  "total_amount": 42.50
}
~~~

**Notes**
- This shows the **semantic result** only. Production results include envelope metadata (dataset id, window bounds, generated_at).
- Dialect nuance: `date_trunc('day', occurred_at)` is treated as UTC for this example. Implementations MUST document dialect/timezone behavior and keep it consistent.

---

## Step 4  Run the report via API (illustrative)

This is a read-only operation (status 🛠). Real endpoints and schemas must match `API.md`.

### Schema note (important)
API payloads MAY accept an analytics profile ref under a field like `profile`.  
Implementations MUST:
- resolve the profile via the Profiles resolver
- freeze the resolved profile hash for the query/run
- record the ref + hash in audit evidence

### Execute analytics query (explicit window bounds)
~~~bash
curl -sS -X POST "https://chartly.example/v1/analytics/queries" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "profile": "tenant-demo/proj-sales::analytics/sales-by-customer@1.0.0",
    "dataset": "sales-events",
    "window_start": "2026-02-03T00:00:00Z",
    "window_end": "2026-02-04T00:00:00Z"
  }'
~~~

### Retrieve results (if async)
~~~bash
curl -sS "https://chartly.example/v1/analytics/queries/qry_123" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/json"
~~~

---

## Step 5  Confirm observability & audit

### Metrics (conceptual)
- analytics_queries_total increases
- analytics_duration_seconds recorded
- write metrics do not increase

### Audit events (conceptual)
- analytics.profile.resolved (ref + resolved hash)
- analytics.query.executed (window bounds + dataset)

If audit events are missing, the report is misconfigured.

---

## Security & safety guarantees

This example guarantees:
- no source data mutation
- no secret access
- no outbound calls to systems **outside Chartlys data plane**
- bounded compute (explicit window bounds)

Violations invalidate the example.

---

## Teardown (safe)

If a derived dataset was created:
1. stop referencing the analytics profile
2. remove or archive the derived dataset
3. keep audit logs

If run as an ad-hoc query only, no teardown is required.

---

## Success criteria

This custom report is complete when:
- analytics profile validates and is versioned
- results match fixture expectations for explicit window bounds
- repeated runs yield identical results
- observability and audit evidence exist
- no source data is mutated

---

## Next steps (🛠)

- Promote this example to  canonical after validation
- Add a golden-result test for the report:
  - load fixture dataset
  - run query with explicit window bounds
  - compare results to golden output
- Reference this example from `API.md` and `PROFILES.md`
