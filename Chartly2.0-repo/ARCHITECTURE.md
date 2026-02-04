# Chartly 2.0  Architecture

## 1) Overview

Chartly 2.0 is a contracts-first, profiles-driven automation system that ingests data from many sources (APIs/domains),
preserves raw truth, normalizes into versioned canonical records, and publishes analytics outputs (charts/reports/projections)
with traceability, quarantine, and immutable audit.

This document defines the production target architecture. Implementation may land incrementally, but **invariants** and
**service boundaries** must remain stable.

---

## 2) Non-negotiable invariants

1. **Contracts-first**  
   Canonical objects and external API payloads are validated against versioned schemas in `contracts/`.

2. **Profiles-over-code**  
   Mapping/cleansing/dedupe/enrichment/retention rules are primarily defined in `profiles/`. Code executes the profile.

3. **Auditability**  
   Every derived canonical record is traceable to raw inputs via object references (bucket/key/hash) and audit records.

4. **Quarantine lane**  
   Invalid or policy-violating data must not enter canonical storage; it is quarantined with reason codes and recovery paths.

5. **Idempotency**  
   Replays and retries must not duplicate canonical records. Idempotency keys are required for ingest and normalize writes.

6. **Backpressure**  
   The system must protect itself and upstreams via queue depth limits, per-source/per-domain rate limiting, and circuit breakers.

---

## 3) Service boundaries

### gateway
**Role:** External HTTP entry point.  
**Responsibilities:**
- Request validation for inbound payloads (where applicable)
- Rate limiting, request IDs, auth proxy/enforcement
- Routes to internal services (orchestrator, analytics, storage, auth)

**Does not:**
- Perform ingestion work, transformation, or store sensitive data.

### orchestrator
**Role:** Control plane for scheduling and workflow coordination.  
**Responsibilities:**
- Builds and executes workflows (DAG/state machine)
- Sharding and load balancing to downstream services
- Retry policy and DLQ routing
- Emits job status events and telemetry

**Does not:**
- Fetch data directly from upstream sources.

### connector-hub
**Role:** High-concurrency upstream connectivity engine.  
**Responsibilities:**
- Connector registry and discovery (where enabled)
- Per-domain concurrency, rate limiting, timeouts
- Circuit breaking and retry strategy for upstream calls
- Writes raw payloads to blob storage (raw plane)
- Emits normalize job events to orchestrator/queue

**Does not:**
- Decide canonical schema mappings or persistence to canonical plane.

### normalizer
**Role:** Raw  canonical conversion with policy enforcement.  
**Responsibilities:**
- Load and compile profiles
- Apply mappings/cleansing/dedupe/enrichment
- Validate canonical outputs against contracts
- Quarantine invalid data
- Write canonical events/metrics/entities to storage
- Append audit records per stage (when audit enabled)

### storage
**Role:** Persistence layer.  
**Responsibilities:**
- Relational metadata storage (sources, jobs, profile versions)
- Canonical record storage interfaces (time-series and relational)
- Blob storage interface (raw and artifacts)
- Cache layer interface (redis)

### analytics
**Role:** Query and reporting plane.  
**Responsibilities:**
- Aggregations and time-series queries
- Report generation and export manifests
- Projections/anomaly detection (incremental)

### audit
**Role:** Immutable audit ledger service.  
**Responsibilities:**
- Append-only audit log
- Hash chain and verification
- Audit query interface (as needed)

### auth
**Role:** AuthN/AuthZ and RBAC policy engine.  
**Responsibilities:**
- JWT/OAuth2/SAML integration (incremental)
- Policy evaluation (roles/permissions)
- Token introspection/validation hooks for gateway

### observer
**Role:** Observability collection/export.  
**Responsibilities:**
- Metrics and tracing export pipelines
- Log ingestion/aggregation hooks (implementation dependent)

---

## 4) Data planes

Chartly maintains three logical planes. Services must not blur boundaries.

### Raw plane (blob storage)
- Stores upstream payloads as immutable blobs.
- Required metadata: `{bucket, key, sha256, content_type, captured_at}`.
- Written by: `connector-hub`.
- Read by: `normalizer` (and optionally `audit`).

### Canonical plane (validated storage)
- Stores versioned canonical objects: metrics/events/entities (and related metadata).
- Written by: `normalizer` (and `storage` services as the interface).
- Read by: `analytics`, `gateway` (query endpoints), and internal services.

### Audit plane (append-only)
- Stores audit records and verification chains.
- Written by: `normalizer`, `orchestrator`, and `connector-hub` (via `audit` service).
- Read by: `gateway`/`analytics` for audit views and compliance exports.

---

## 5) Control plane (orchestrator)

The orchestrator owns:
- Scheduling (cron/interval/event-driven)
- Workflow state machine (queued  running  succeeded/failed/quarantined)
- Sharding strategy (consistent hashing by tenant_id + source_id)
- Retry policy and DLQ semantics

**DLQ principle:** a poisoned job must not block healthy work. DLQ items require explicit quarantine/recovery handling.

---

## 6) Execution flows

### 6.1 Ingest (happy path)

ASCII sequence:

gateway
  |
  |  (ingest request / schedule)
  v
orchestrator
  |
  |  (dispatch job)
  v
connector-hub
  |
  |  (fetch upstream + enforce limits)
  v
storage (raw plane blob)
  |
  |  (emit normalize job + raw_ref)
  v
normalizer
  |
  |  (profile mapping + validation)
  v
storage (canonical plane)
  |
  |  (append audit records)
  v
audit (append-only)

### 6.2 Ingest (failure path)

- Upstream timeout/error  connector-hub applies retry/backoff; if exhausted  DLQ + job failed state.
- Invalid schema or policy violation  normalizer quarantines and emits reason code; canonical write is blocked.
- Storage unavailable  orchestrator pauses/backs off; connector-hub and normalizer stop accepting work (backpressure).

---

### 6.3 Query / report (happy path)

gateway
  |
  |  (query/report request)
  v
analytics
  |
  |  (read canonical + compute aggregates)
  v
storage
  |
  |  (return results / artifact refs)
  v
gateway  client

---

## 7) Interfaces and contracts

**Contracts source:** `contracts/v1/*`.

Minimum enforcement:
- Normalizer MUST validate canonical objects against `contracts/v1/canonical/*`.
- Gateway MUST validate report/projection request payloads against `contracts/v1/reports/*` (when those endpoints exist).
- Telemetry payloads should follow `contracts/v1/telemetry/*`.

---

## 8) Scaling model

- Horizontal scaling for all stateless services: gateway, orchestrator, connector-hub, normalizer, analytics.
- connector-hub enforces:
  - per-domain concurrency caps
  - per-source/per-domain rate limits
  - circuit breakers (open/half-open/closed)
- Backpressure signals:
  - queue depth thresholds
  - downstream latency/availability
  - worker saturation
- Sharding:
  - consistent hashing by `{tenant_id, source_id}` to improve cache locality and reduce hot-spotting.

---

## 9) Security model

- RBAC baseline enforced at gateway; policy decisions delegated to auth when enabled.
- Secrets never stored in repo configs/profiles; sourced from env vars or secret manager.
- PII/PHI constraints are enforced by profiles and normalizer policy checks; violations route to quarantine.

---

## 10) Observability model

- Structured logs include: `ts, level, service, env, tenant_id, request_id, job_id, source_id`.
- Metrics include: ingest_rate, error_rate, queue_depth, latency percentiles, connector health.
- Tracing correlates request_id and job_id across service boundaries.

---

## 11) Deployment topology

### Local
- `docker-compose.yml` provides dependencies and (optionally) service containers as implemented.

### Production
- Kubernetes deployments per service (HPA enabled).
- Separate stateful stores: Postgres, Redis, blob store, time-series store.
- Helm chart in `infra/helm/chartly/`, Terraform in `infra/terraform/`.

---

## 12) Operational playbooks

### Incident: Upstream source outage
- connector-hub circuit breaker opens.
- orchestrator reduces schedule frequency for the source.
- alert on sustained failure rate.

### Incident: Queue depth increasing
- verify downstream availability (storage, normalizer)
- scale normalizer/connector-hub horizontally
- reduce dispatch rate / tighten backpressure thresholds

### Incident: Schema drift (upstream changes)
- connector-hub flags drift event
- normalizer quarantines invalid payloads with reason codes
- update profile mappings and reprocess from raw plane

### Incident: Duplicate records
- verify idempotency key configuration
- verify dedupe strategy in profiles
- run remediation via re-compaction/dedup job (when implemented)
