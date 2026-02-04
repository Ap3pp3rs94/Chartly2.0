# Profile Versioning Policy  Chartly 2.0

Profiles are versioned configuration bundles that control raw  canonical behavior.
Because profiles affect data correctness and compliance, profile changes must be treated as carefully as code changes.

This document defines how profile versions are authored, validated, promoted, and pinned.

---

## Where the version lives

Each profile bundle must declare a version in:

- `profiles/**/profile.yaml`

Recommended fields:
- `name`
- `domain`
- `version` (string)
- `compatibility` (constraints/notes)
- `pii_phi` (classification)
- `owners`

Version format:
- Use semantic versioning: `MAJOR.MINOR.PATCH` (e.g., `1.2.0`)
- `MAJOR` increases for breaking mapping/rule changes
- `MINOR` for backward-compatible additions
- `PATCH` for non-behavioral fixes or clarifications

---

## What counts as breaking vs non-breaking

### Non-breaking profile changes (usually MINOR/PATCH)
- Adding new optional mappings (that do not change existing outputs)
- Adding new metric names while keeping existing metric semantics intact
- Adding cleansing rules that only improve normalization without changing meaning
- Adding new alert definitions
- Clarifying documentation/metadata fields

### Breaking profile changes (MAJOR)
- Changing how a field maps to a canonical metric/event/entity
- Renaming canonical metric names used by downstream analytics
- Changing units (e.g., ms  seconds) without a new metric name
- Tightening validation rules that will quarantine previously accepted records
- Changing dedupe/idempotency keys in a way that affects duplicate behavior
- Changing PII/PHI classification or handling requirements

When in doubt, treat the change as breaking.

---

## Promotion flow (local  staging  production)

Recommended promotion steps:

1) **Author locally**
- Create/modify profile files under `profiles/`.
- Update `profile.yaml:version`.

2) **Validate**
- Run profile lint rules (in `profiles/tests/` and `tools/profiler/`).
- Validate mappings against sample payload fixtures (recommended).

3) **Controlled ingest**
- Run ingestion against a small source set or recorded payloads.
- Confirm outputs validate against contracts and meet expectations.

4) **Promote to staging**
- Merge via PR with CODEOWNERS review.
- Deploy staging with the new profile version pinned for test tenants/sources.

5) **Promote to production**
- Roll out gradually:
  - per tenant
  - per source
  - behind feature flags if needed
- Record promotion in changelog/release notes.

---

## Pinning profile versions (per source / tenant)

Profiles should be pin-able by:
- tenant
- source
- connector type
- domain

Pinning rules (recommended):
- Default to the latest approved profile version for a domain.
- Allow explicit pinning for stability and reproducibility.
- Store the profile version used in:
  - canonical records (as metadata)
  - quarantine records
  - audit records for pipeline stages

---

## Validation requirements (tooling)

Minimum validation expectations:
- Profile YAML must be syntactically valid.
- Required profile files must exist.
- Mappings must produce contract-valid outputs for known fixtures.
- Deduplication configuration must be well-formed and deterministic.

Validation inputs:
- `profiles/tests/fixtures/` (example mappings/rules)
- `contracts/validators/fixtures/` (payload validation fixtures)

---

## Rollback policy

Rollbacks must be safe and auditable:
- Keep prior profile versions available.
- Maintain migration notes for breaking changes.
- If a profile causes unexpected quarantine or data drift:
  - rollback by pinning sources/tenants back to the previous version
  - do not delete canonical data; instead mark segments for reprocessing if required

---

## Governance

Profile changes require:
- PR review by CODEOWNERS
- a version bump
- documentation of behavior changes for breaking versions
- audit trail for production rollouts (roadmap)

Profiles are part of the systems law; treat them as such.
