# Chartly 2.0  Profiles

## Contract status & trust model

This document defines **what a Profile is**, how profiles are resolved, and the rules that make profiles safe to use in automation.

### Legend
-  **Implemented**  verified in code and/or conformance tests
- 🛠 **Planned**  desired contract, may not exist yet
- 🧪 **Experimental**  available but may change without full deprecation guarantees

**Rule:** Anything not explicitly marked  is 🛠.

### Promotion criteria (🛠  )
A profile capability becomes  only when:
- the loader/resolver exists,
- the merge rules are enforced,
- and at least one conformance test verifies determinism (same inputs  same resolved profile).

---

## Why profiles exist

Profiles turn configuration sprawl into **portable, versioned, reviewable contracts**.

A Profile is:
- **declarative**: describes behavior, not a script to run
- **versioned**: safe to reference from workflows without surprise changes
- **portable**: provider-neutral; deployable via Git/files/bundles; Kubernetes is one packaging option
- **auditable**: profile changes are change events with owners, reviews, and traceability

Profiles are Chartlys way to make pipelines feel like a **platform runtime**:
- consistent connector behavior
- consistent normalization rules
- repeatable analytics and storage policies
- predictable operations

---

## Core principles

1. **Deterministic resolution**
   Given the same profile ref + overlays + environment, the resolved result MUST be identical.

2. **No secrets in profiles**
   Profiles may reference secret locations (`secretRef`) but never contain secret values.

3. **Separation of concerns**
   Profiles define behavior; workflows define orchestration; infrastructure defines runtime.

4. **Composable overrides**
   Base profiles can be overridden by environment/project overlays in a controlled, explicit way.

5. **Contract-first evolution**
   Profile schemas evolve with explicit compatibility guarantees.

---

## Mental model

~~~text
     (authn/authz)          (control plane)                (data plane)
Client/Workflow  ref  Profile Resolver  resolved  Service Behavior

                         
                           Base Profile   (versioned)
                         
                                  merge (deterministic)
                         
                            Overlay(s)    (env/project)
                         
                                  validate + freeze
                         
                          Resolved Spec   (immutable for a run)
                         
~~~

A resolved profile is the **exact configuration** used for a run, and should be stored (or at least hash-addressed) for auditability.

---

## Glossary

- **Profile**: A declarative config object (connector/normalizer/analytics/etc.) with metadata and a `spec`.
- **Profile Ref**: A string that identifies a profile by kind/name/version (and optionally variant).
- **Overlay**: An additive/override layer applied on top of a base profile (environment/project specific).
- **Resolved Profile**: The fully merged, validated spec used at runtime.
- **Bundle**: A packaging unit that groups multiple profiles (and optional test fixtures) for distribution.
- **Namespace**: The scope within which a profile ref resolves (tenant/project/global).

---

## Profile types

Profiles are typed so ownership and validation rules are clear.

| Kind (type) | Used by | Purpose | Typical owner |
|---|---|---|---|
| `connector` | Connector Hub | Ingest behavior: endpoints, pagination, rate limits, auth mode references | Integration / Platform |
| `normalizer` | Normalizer | Canonical mapping rules and schema shaping | Data Engineering |
| `analytics` | Analytics | Saved query templates, rollups, metrics definitions | Analytics / Data |
| `storage` | Storage | Retention, partitioning, dataset policy defaults | Platform / Data |
| `rbac` | Auth | Role definitions and permission bundles | Security / Platform |
| `observer` | Observer | SLO/health policies, alert thresholds, scrape allowlists | Platform / SRE |

**Note:** A kind defines the validation schema and which service consumes it.

---

## Profile reference format

### Recommended profile ref syntax (🛠)
~~~text
<namespace>::<kind>/<name>@<version>[#<variant>]
~~~

Examples:
~~~text
global::normalizer/sales-default@1.2.0
tenant-acme/proj-retail::connector/http-json@0.9.0#paged
tenant-acme/proj-retail::analytics/daily-rollup@2.0.1
global::storage/timeseries-default@1.0.0
~~~

### Namespace resolution rule (contract)
Profiles resolve within a namespace:
- `tenant/<tenant_id>/project/<project_id>` (project scope), and optionally
- `global` (platform scope)

Resolution MUST be deterministic:
1. project namespace
2. global namespace (fallback)

Conflicts are forbidden:
- if two profiles with the same `<kind>/<name>@<version>` exist in the same namespace, resolution MUST fail.

### Ref rules
- `kind` MUST be one of the known kinds.
- `name` SHOULD be DNS-label safe (`[a-z0-9-]`) for portability.
- `version` SHOULD be SemVer (`MAJOR.MINOR.PATCH`).
- `#variant` is optional and intended for small deltas without cloning the whole profile.
- Avoid ambiguous refs like `@latest`. If used, they MUST resolve to an immutable version for the lifetime of a run.

---

## Profile object format

Profiles are YAML-first but may be JSON equivalent.

### Common header (contract)
~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: normalizer              # profile kind (connector/normalizer/analytics/storage/rbac/observer)
  name: sales-default           # profile name
  version: 1.2.0                # semver for profile CONTENT evolution
  description: Canonical mapping for sales events
  owners:
    - team:data-platform
  labels:
    tier: stable
  annotations:
    change_ticket: CHARTLY-1234
spec:
  # kind-specific shape
~~~

### apiVersion vs metadata.version (contract)
- `apiVersion` changes ONLY when the **profile schema** changes in a breaking way (wire format / field meaning).
- `metadata.version` changes when **profile content** changes (behavior/config), following SemVer within that schema.

### Required invariants
- `metadata.kind`, `metadata.name`, `metadata.version` MUST be present.
- `spec` MUST exist and MUST validate against the schema for `metadata.kind`.
- Unknown top-level fields SHOULD be rejected (strict mode) to prevent silent misconfig.

---

## Resolution & override rules

### Resolution precedence (🛠)
When a service needs a profile, resolution happens in this order:

1. **Explicit ref on the request** (workflow step, API request)
2. **Project default** (project settings / environment policy)
3. **Service default** (built-in baseline)

If no profile is resolved, the request MUST fail with a clear error.

### Overlay application (🛠)
Overlays are applied in a deterministic order:
1. base profile
2. environment overlay (e.g., `dev`, `prod`)
3. project overlay
4. request-scoped overlay (rare; must be explicit)

### Deterministic merge rules (contract)
- **Maps/objects**: deep-merge by key
- **Scalars**: overlay replaces base
- **Lists/arrays**: replace-by-default (no implicit concatenation)
- **Explicit list merge** (optional): allow `spec.merge.lists: append|unique` only if the schema explicitly permits it

If a merge produces an invalid spec, resolution MUST fail before execution.

### Immutability for runs
Once a run starts, the resolved profile is **frozen** for that run:
- changes to profile sources MUST NOT affect an in-flight run
- a run should store at least:
  - the profile ref(s)
  - resolved hash (e.g., sha256 of the resolved YAML/JSON)
  - and optionally the resolved spec blob (recommended for audit/debug)

---

## Distribution & packaging

Profiles are provider-neutral artifacts. Common delivery mechanisms:

### Git / filesystem (🛠)
- Profiles live in a repo path or bundle directory
- Promotion via pull request + review + tests
- Deployed by copying/syncing files to the runtime (any mechanism)

### Kubernetes-native (🛠)
- Profiles stored in ConfigMaps (non-secret)
- Mounted as files or loaded via API from a profile registry service
- Labeled for discovery:
  - `chartly.io/profile-kind=<kind>`
  - `chartly.io/profile-name=<name>`
  - `chartly.io/profile-version=<version>`

### Bundle artifacts (🛠)
- A profile bundle is a directory or packaged artifact containing:
  - multiple profiles
  - optional fixtures for tests
  - optional compatibility notes

Example bundle layout:
~~~text
profiles/
  normalizer/
    sales-default/1.2.0/profile.yaml
  connector/
    http-json/0.9.0/profile.yaml
  analytics/
    daily-rollup/2.0.1/profile.yaml
  tests/
    fixtures/
      sales-events.sample.json
    golden/
      sales-default.resolved.yaml
~~~

---

## Validation & testing

### Validation gates (recommended)
- Schema validation for `metadata.kind`
- Strict unknown-field rejection
- Deterministic resolution test (same inputs  same resolved output)
- Golden file test for critical profiles (resolved spec snapshot)
- Compatibility checks:
  - within a major version: additive fields allowed
  - breaking changes require version bump

### Conformance fixture spec (one-page, contract)

This is the minimum spec required to promote determinism-related behaviors to .

**Fixture inputs**
- `base.yaml`  base profile
- `overlay-env.yaml`  environment overlay (e.g., prod)
- `overlay-project.yaml`  project overlay
- `context.json`  immutable context values used during resolution (namespace, env, project id)

**Test: deterministic merge**
1. Resolve the profile twice with identical inputs.
2. Serialize resolved output using canonical rules:
   - UTF-8
   - stable key ordering (lexicographic)
   - normalized line endings (`\n`)
3. Compute `sha256` of the canonical bytes.
4. Assert both runs produce identical:
   - resolved bytes
   - resolved hash
   - resolved validation result

**Test: list replace-by-default**
- If `base.spec.list = [a,b]` and overlay sets `[c]`, resolved MUST equal `[c]`.

**Test: unknown-field rejection**
- Add `spec.unknownField = true` and assert resolution fails with a schema error.

**Golden output (optional but recommended)**
- Store `resolved.yaml` and `resolved.sha256`.
- CI asserts the newly resolved output matches golden unless a deliberate update was reviewed.

---

## Security posture for profiles

Profiles are configuration, not a secret store.

### Hard rules
- No secret values in `spec` or annotations.
- Auth material MUST be referenced, never embedded:
  - `secretRef` is allowed, containing only a name/key pointer.
- Profiles MUST NOT contain executable code (no scripts/macros).
- Connector profiles MUST:
  - declare timeouts
  - declare max payload size expectations (or rely on a platform default)
  - be subject to egress allowlists / DNS allowlists
  - include SSRF guardrails (block localhost, link-local, and metadata IP ranges)

### Recommended runtime enforcement
- Admission checks (or startup checks) reject profiles that:
  - include suspicious fields (e.g., `token`, `password`, `private_key`) unless explicitly part of a `secretRef` object
  - reference paths outside allowed mount roots (if file-based)
- Audit every profile change:
  - who changed what
  - which version was promoted
  - which runs used it

---

## Observability & audit integration

To make profiles operable:
- Every run SHOULD emit:
  - `profile_ref` (namespace + kind/name/version)
  - `profile_hash` (resolved hash)
- Metrics SHOULD include the profile kind and name, but avoid high-cardinality labels like full version on hot paths unless sampling is used.
- Audit events SHOULD record:
  - profile publish/promote actions
  - profile resolution failures
  - profile used for each run

---

## Example profiles (no secrets)

### 1) Connector profile: HTTP JSON with paging (guardrails included)
~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: connector
  name: http-json
  version: 0.9.0
  description: Generic HTTP JSON connector with cursor paging
  owners: [team:integrations]
  labels: {tier: stable}
spec:
  transport:
    baseUrl: "https://api.example.invalid"
    timeoutMs: 15000
    maxPayloadBytes: 1048576
    headers:
      Accept: "application/json"
  egressPolicy:
    dnsAllowlist:
      - "api.example.invalid"
    blockCidrs:
      - "127.0.0.0/8"
      - "169.254.0.0/16"
      - "10.0.0.0/8"
      - "172.16.0.0/12"
      - "192.168.0.0/16"
  auth:
    mode: bearer
    secretRef:
      name: chartly-connector-http-json
      key: token
  pagination:
    mode: cursor
    requestParam: cursor
    responseField: next_cursor
  rateLimit:
    maxRequestsPerMinute: 600
  retry:
    maxAttempts: 5
    backoffMs: 500
~~~

### 2) Normalizer profile: Sales events canonicalization
~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: normalizer
  name: sales-default
  version: 1.2.0
  description: Map inbound sales payloads into canonical events
  owners: [team:data-platform]
  labels: {tier: stable}
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

### 3) Analytics profile: Daily rollup
~~~yaml
apiVersion: chartly.io/v1alpha1
kind: Profile
metadata:
  kind: analytics
  name: daily-rollup
  version: 2.0.1
  description: Daily rollup query template for sales totals
  owners: [team:analytics]
  labels: {tier: stable}
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

## Operator checklist

Before promoting a profile to stable:
- [ ] Schema validates; unknown fields rejected
- [ ] No secrets embedded; only `secretRef`
- [ ] Timeouts and retries are set (no infinite waits)
- [ ] Connector egress allowlist + SSRF guardrails are configured
- [ ] Max payload guidance is present (`maxPayloadBytes` or platform default)
- [ ] Deterministic resolution test passes
- [ ] Golden resolved output updated (if used)
- [ ] Owners reviewed and approved changes
- [ ] Changelog note for the new version exists
- [ ] A rollback version is identified (previous stable)

---

## Next steps (🛠)

- Add a minimal profile resolver that:
  - loads profiles from a portable source (files, ConfigMaps, or bundles)
  - applies deterministic overlays
  - returns a resolved hash + resolved spec
- Add conformance tests:
  - deterministic merge behavior
  - schema strictness
  - no secrets enforcement
  - SSRF/egress rules for connector profiles
- Add an audit event schema for:
  - profile publish
  - profile promotion
  - profile resolution failure
