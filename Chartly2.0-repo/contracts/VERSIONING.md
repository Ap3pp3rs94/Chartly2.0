# Contract Versioning Policy  Chartly 2.0

This document defines how Chartly contracts (JSON Schemas) evolve over time.

Contracts are versioned and treated as a **public interface**:
- Canonical objects (entities/events/metrics/etc.)
- Telemetry payloads
- Report/projection request and response payloads

Breaking contract changes are controlled and explicit.

---

## Version model

### Major versions
Each major version lives in its own directory:

- `contracts/v1/...`
- `contracts/v2/...`

A new major version indicates **breaking changes** may exist relative to the previous major version.

### Minor/patch changes
Within a major version (e.g., `v1`), changes are expected to be **backward compatible**.
Chartly uses semantic discipline even though schemas themselves do not have a built-in minor/patch identifier:
- The git history and release tags document minor/patch evolution.
- The directory name represents major compatibility only.

---

## Backward compatibility (required within a major)

Within `contracts/v1`, changes MUST NOT break existing producers/consumers that correctly implement v1.

### Allowed changes (v1)
- Add new **optional** fields.
- Add new schema files (new object types).
- Expand enums where existing values remain valid.
- Add new `oneOf` branches that preserve prior behavior.
- Add constraints that do not reject previously valid payloads (use caution).

### Forbidden changes (v1)
- Remove required fields.
- Make an optional field required.
- Rename fields (without keeping the old field).
- Change data types of existing fields.
- Change meaning/semantics of existing fields without creating a new field name.
- Tighten validation in a way that makes previously valid payloads invalid.

---

## Deprecation policy

Deprecation within a major version is allowed if it is **non-breaking**:

- Mark fields as deprecated in documentation (and optionally schema annotations).
- Continue accepting deprecated fields until:
  - A new major version is released, or
  - A migration window has completed for all supported deployments/tenants.

Deprecation must be:
- Documented in the changelog
- Reflected in fixtures (include deprecated forms when relevant)
- Enforced only via warnings until a major version change

---

## Breaking changes (new major version)

A breaking contract change requires a new major directory:

1) Create `contracts/v2/...`
2) Copy and evolve schemas as required
3) Add v2 fixtures under `contracts/validators/fixtures/`
4) Update code to support:
   - reading/validating both v1 and v2 when necessary
   - migrating canonical storage and APIs safely

**Rule:** never modify v1 to include a breaking change.

---

## Migration strategy (service behavior)

Services may need to handle multiple major versions during transition.

Recommended migration path:
1) **Dual-read**: accept and validate both v1 and v2 payloads (gateway and normalizer).
2) **Canonicalize**: normalize all inputs to a chosen internal representation per tenant/version policy.
3) **Dual-write (optional)**: write both v1 and v2 outputs if consumers require it.
4) **Cutover**: move tenants/clients to v2 behind a feature flag or explicit configuration.
5) **Sunset**: after migration window, disable v1 for tenants that have moved.

Version selection should be explicit:
- per-tenant
- per-source
- per-endpoint (reports/projections)

---

## Validation and fixtures

Contracts must be validated continuously.

Minimum requirements:
- Each schema should have at least one representative fixture payload.
- Fixtures must validate against their schema for the target major version.
- Validation must run in CI (when validator tooling is implemented).

Fixtures live in:
- `contracts/validators/fixtures/`

Validator entry point:
- `contracts/validators/validate.py` (intended to validate fixtures against schemas)

---

## Review and governance

Changes to contracts require:
- CODEOWNERS review
- Changelog entry
- Clear documentation of compatibility impact

Contract changes are project-law changes and must be treated with the same rigor as API changes.
