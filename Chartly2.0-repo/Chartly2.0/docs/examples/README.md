# Chartly 2.0  Examples

## Purpose

This directory contains **authoritative examples** that demonstrate how Chartly is intended to be used in practice.

Examples are **not tutorials** and **not experiments**.  
They exist to:

- show correct, idiomatic usage of Chartly contracts
- validate documentation against reality
- serve as fixtures for tests and onboarding
- prevent docs drift by providing concrete references

If an example contradicts a document, **the example is wrong** unless explicitly marked experimental.

---

## Trust model

Examples are treated as **contract evidence**.

### Legend
-  **Canonical**  matches current contracts and SHOULD be followed
- 🛠 **Planned**  illustrates an intended pattern, not yet fully implemented
- 🧪 **Experimental**  may change; do not rely on for production

**Rule:** Any example without an explicit marker is 🛠 by default.

### Precedence rule (authoritative)
If a **conformance test** and an **example** conflict:
- the **test is authoritative**
- the example MUST be updated or removed

Examples may explain behavior; tests define it.

---

## What belongs here (and what doesnt)

### Examples SHOULD:
- be minimal but complete
- reference versioned profiles (never `latest`)
- obey all security, scaling, and deployment invariants
- be copy/paste safe

### Examples MUST NOT:
- include secrets or credentials
- rely on undeclared infrastructure
- bypass profiles, RBAC, or security rules
- encode business-specific assumptions

### Copy/paste safe (clarified)
Copy/paste safe means:
- examples either run successfully **or**
- fail **explicitly and early** with clear errors when prerequisites are missing

Silent partial success or undefined behavior is forbidden.

---

## Directory structure (recommended)

~~~text
examples/
  workflows/
    simple-ingest.yaml
    daily-rollup.yaml
  profiles/
    connector-http-json.yaml
    normalizer-sales.yaml
    analytics-daily-rollup.yaml
  deployments/
    dev-values.yaml
    staging-values.yaml
  api/
    create-workflow.http
    run-workflow.http
  fixtures/
    input.sample.json
    expected.resolved.yaml
~~~

Not all directories must exist. Add examples **only when they teach something new**.

---

## Example categories

### 1) Workflow examples
Show how workflows reference profiles and define execution.

**Must demonstrate:**
- explicit profile refs
- deterministic behavior
- idempotent execution

### 2) Profile examples
Show valid profile objects for each kind.

**Must demonstrate:**
- schema correctness
- versioning discipline
- security rules (`secretRef`, egress allowlists)

### 3) API examples
Show raw API interactions.

**Must demonstrate:**
- request/response envelopes
- error handling
- idempotency and retries

### 4) Deployment examples
Show environment-specific configuration.

**Must demonstrate:**
- immutable promotion
- no secrets in manifests
- environment isolation

---

## Canonical example rules

An example MAY be marked **canonical** only if:

- it validates against the current contract
- it has been exercised manually or via test
- it matches at least one of:
  - `API.md`
  - `PROFILES.md`
  - `CONNECTORS.md`
  - `DEPLOYMENT.md`
  - `SCALING.md`
  - `SECURITY.md`

Canonical examples SHOULD be referenced directly from the docs they support.

---

## Example annotations

Examples MAY include a header comment:

~~~yaml
# status: canonical
# last_verified: 2026-02-02
# related_docs:
#   - API.md
#   - PROFILES.md
~~~

Annotations are **metadata only** and MUST NOT affect runtime behavior.

---

## Validation expectations

Examples SHOULD be validated by automation where possible:

- schema validation (profiles, workflows)
- dry-run execution (deployment examples)
- golden output comparison (fixtures)

Examples that fail validation MUST be fixed or removed.

---

## Security guarantees

All examples in this directory guarantee:

- no secrets or credentials
- no production endpoints
- no destructive operations by default

If an example violates these guarantees, it MUST be clearly labeled 🧪.

---

## Operator guidance

When onboarding a new user or contributor:

1. Start with examples before reading all docs.
2. Use examples as a reference for what right looks like.
3. If unsure, **copy an example and modify it** rather than inventing a new pattern.

---

## Planned end-to-end example (🛠)

A single canonical end-to-end example SHOULD demonstrate:

- single-tenant, non-production scope
- bounded dataset (small, deterministic input)
- connector  normalizer  analytics  storage
- observable outputs and audit trail
- safe teardown / no persistent side effects

This example will act as the primary onboarding and regression fixture.

---

## Next steps (🛠)

- Promote one end-to-end example to  canonical
- Add example validation to CI
- Cross-link canonical examples from core docs
