# Chartly 2.0  Contracts

Contracts are the **source of truth** for data shapes and externally-facing payloads in Chartly 2.0.

Chartly is **contracts-first**:
- Canonical outputs (entities, events, metrics, etc.) must validate against versioned schemas.
- Externally facing request/response payloads (reports, projections) should validate at the gateway boundary.
- Telemetry payloads should conform to telemetry schemas to keep observability consistent.

> The authoritative compatibility and change rules live in `contracts/VERSIONING.md`.

---

## Directory layout

- `contracts/v1/canonical/`  
  Canonical object schemas:
  - Source
  - Entity
  - Event
  - Metric
  - Case
  - Artifact
  - AuditRecord

- `contracts/v1/telemetry/`  
  Operational/health schemas:
  - system metrics
  - api stats
  - ingestion health
  - queue stats

- `contracts/v1/reports/`  
  Report and projection request/response schemas:
  - chart config
  - projection request
  - export manifest
  - report result

- `contracts/validators/`  
  Validation utilities. `validate.py` is the entry point when implemented.

- `contracts/validators/fixtures/`  
  Example payloads used for contract tests. Fixtures are expected to validate against the schemas they represent.

-- `contracts/codegen/`  
  Type generation scripts (iterative) for:
  - Go structs
  - TypeScript types
  - Python models

---

## Where contracts are enforced

At minimum, the architecture enforces schemas in these locations:

- **Normalizer (required):** validates canonical outputs against `contracts/v1/canonical/*`.
- **Gateway (when endpoints exist):** validates inbound report/projection payloads against `contracts/v1/reports/*`.
- **Telemetry (recommended):** services emit telemetry that conforms to `contracts/v1/telemetry/*`.

---

## Changing contracts safely

Within a major version (e.g., `v1`), changes must be **backward compatible**.

Allowed (typical):
- Adding optional fields
- Adding new schema files
- Expanding enums where safe

Not allowed within `v1`:
- Removing required fields
- Renaming fields
- Changing semantics of an existing field without introducing a new field

If a breaking change is required:
- Create `contracts/v2/` and introduce v2 schemas there.
- Maintain compatibility and migrations at the service level.

---

## Adding v2 (breaking contracts)

1) Create new directories:
- `contracts/v2/canonical/`
- `contracts/v2/telemetry/`
- `contracts/v2/reports/`

2) Update:
- `contracts/VERSIONING.md` with v2 rules
- Fixtures for v2 under `contracts/validators/fixtures/`

3) Ensure services can:
- Read v1 and v2 where required
- Upgrade in a controlled manner (feature flags / tenant gates)

---

## Fixtures and validation

Fixtures serve two goals:
1) Demonstrate expected payload shapes.
2) Provide stable inputs for contract validation tests.

Recommended workflow:
- Add/modify schema
- Add/modify fixture(s)
- Run validator (when implemented) to confirm compatibility

---

## Code generation (iterative)

The long-term goal is to generate consistent SDK types from contracts:
- Go models for services and shared `pkg/` types
- TypeScript types for `web/` and TS SDK
- Python models for tooling and the Python SDK

These scripts live in `contracts/codegen/` and should be treated as build tooling, not runtime dependencies.
