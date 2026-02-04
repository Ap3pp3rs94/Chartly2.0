# Chartly Tool  Schema Generator

## Contract status & trust model

**schema-gen** is a governed tool that produces **contract artifacts** (schemas, OpenAPI, and contract manifests) deterministically, so Chartlys contracts remain enforceable, reviewable, and testable.

### Legend
-  **Implemented**  tool exists and passes conformance tests
- 🛠 **Planned**  intended behavior, not yet implemented
- 🧪 **Experimental**  may change; do not rely on in automation

**Rule:** Anything not explicitly marked  is 🛠.

### Promotion criteria (🛠  )
schema-gen becomes  only when:
- generation is byte-for-byte deterministic,
- drift detection is enforceable in CI,
- write operations are gated by `--apply`,
- and conformance tests validate exit codes and stability.

---

## What schema-gen is (and is not)

### schema-gen IS
- a deterministic generator of **schema and contract artifacts**
- a verifier for schema drift (generated vs committed)
- a producer of stable, content-addressed artifacts
- a contract enforcement gate for CI and review

### schema-gen IS NOT
- a runtime migration tool
- a secret management tool
- a build system replacement
- a provider-specific deployment utility

---

## Why this tool exists

Chartly treats contracts as evidence. schema-gen makes that enforceable by:

- generating canonical schemas from source contracts
- producing stable hashes for review and CI
- preventing silent drift between docs, schemas, and code

---

## Inputs and outputs

### Inputs (roadmap)
- OpenAPI source (e.g., `services/gateway/openapi/openapi.yaml`)
- Profile schemas (per kind)
- Workflow schemas
- Canonical event schema(s)
- Contract metadata (version, notes)

### Outputs (roadmap)
- JSON Schema files
- Canonicalized OpenAPI output
- A manifest recording:
  - input paths and hashes
  - generator version
  - output hashes

---

## Determinism guarantees (binding)

Given the same:
- input files
- tool version
- flags

schema-gen MUST produce byte-identical:
- generated schema files
- OpenAPI output
- manifest and hash files

### Deterministic rules
- stable key ordering
- stable list ordering
- no timestamps other than explicit version metadata
- no environment-dependent paths in output

---

## CLI interface

### Command shape
~~~text
chartly-tool schema-gen <plan|generate|verify> [flags]
~~~

---

## Command semantics (explicit)

### `plan`
- Always read-only
- Always offline
- Implies `--dry-run`
- Shows what *would* be generated and the expected hashes

### `verify`
- Always read-only
- Always offline
- Regenerates artifacts in-memory deterministically
- Compares hashes to committed artifacts
- Exit `4` on drift

### `generate`
- Writes artifacts to disk
- **REQUIRES `--apply`**
- `--dry-run` may be used to show writes without performing them

---

## Required flags (roadmap)
- `--path <repo-root-or-contracts-root>`
- `--format <json|text>`

### Optional flags
- `--apply` (required for `generate`)
- `--dry-run`
- `--out <path>` (default: `contracts/`)
- `--openapi <path>`
- `--schemas <path>`
- `--include <pattern>` / `--exclude <pattern>`
- `--manifest <path>` (default: `contracts/manifest.json`)
- `--redact` (explicitly allow redaction; see below)

---

## Exit codes (contract)

- `0` success
- `1` general error
- `2` invalid arguments / usage error
- `3` precondition failed (missing inputs)
- `4` validation failed (drift, invalid schema, or secret detection)
- `5` unsafe operation blocked (missing `--apply`)

---

## Redaction vs fail-fast (explicit precedence)

### Default behavior (strict)
- schema-gen **MUST fail fast** (exit `4`) if secret-like fields or values are detected in inputs.
- No artifacts are generated.

### Optional behavior (`--redact`)
- When `--redact` is explicitly provided:
  - secret-like fields are deterministically redacted
  - generation proceeds
  - redaction is recorded in the manifest

**Rule:** Redaction is opt-in. Fail-fast is the default.

---

## OpenAPI canonicalization rules (binding)

When generating `openapi.normalized.json`, schema-gen MUST:

- sort all object keys deterministically
- sort components (`schemas`, `parameters`, `responses`, etc.) by name
- normalize `$ref` formatting (no relative-path ambiguity)
- remove non-semantic fields that cause drift:
  - examples
  - descriptions (optional, but must be consistent)
  - vendor extensions (`x-*`) unless explicitly included
- preserve semantic fields:
  - paths
  - operations
  - request/response schemas
  - security definitions

These rules exist to prevent nondeterministic OpenAPI tooling output.

---

## Output artifact layout (proposal)

~~~text
contracts/
  manifest.json
  openapi/
    openapi.normalized.json
  schemas/
    canonical/
      event.schema.json
    profiles/
      connector.schema.json
      normalizer.schema.json
      analytics.schema.json
      storage.schema.json
      rbac.schema.json
      observer.schema.json
    workflows/
      workflow.schema.json
  hashes/
    manifest.sha256
    openapi.normalized.sha256
    schemas.sha256
~~~

### Manifest requirements
The manifest MUST include:
- generator version
- input paths and input hashes
- output hashes
- contract version string (if provided)
- redaction flag (if used)

---

## Drift detection & verification

`verify` mode MUST:
- regenerate artifacts in-memory deterministically
- compare hashes to committed artifacts
- exit `4` on any mismatch

This is the CI enforcement gate for contracts.

---

## Conformance tests required for 

- determinism test (same inputs  byte-identical outputs)
- verify drift test (modified input  exit `4`)
- missing input test (exit `3`)
- unsafe write blocked (generate without `--apply`  exit `5`)
- redaction test:
  - default fail-fast
  - `--redact` produces deterministic redacted output
- OpenAPI canonicalization test
- manifest schema validation test

---

## Next steps (🛠)

- Implement `plan` first (dependency-free, deterministic)
- Implement `verify` next (CI enforcement)
- Implement `generate` last with strict `--apply` gating
- Add at least one golden fixture for OpenAPI normalization and one for schema output
