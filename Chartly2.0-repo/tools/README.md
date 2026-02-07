# Chartly 2.0  Tools

## Contract status & trust model

This document defines the **governance contract** for everything under `tools/`: developer and operator utilities that support Chartly without becoming an unreviewed side-channel.

### Legend
-  **Implemented**  tool exists and has at least one conformance test
- 🛠 **Planned**  intended tool or behavior, not yet implemented
- 🧪 **Experimental**  may change; do not rely on in automation

**Rule:** Anything not explicitly marked  is 🛠.

### Promotion criteria (🛠  )
A tool becomes  only when:
- it is deterministic (same inputs  same output),
- it is safe-by-default (no destructive action without explicit flags),
- it supports `--dry-run` where applicable,
- and at least one test validates exit codes and stable output.

---

## What belongs in `tools/` (and what does not)

### `tools/` SHOULD contain
- CLI utilities for:
  - validating contracts (profiles, manifests, examples)
  - resolving profiles and printing hashes
  - environment diff and drift detection
  - smoke checks and conformance runs
  - fixture loading for tests (non-production only)
- Small scripts that wrap reproducible commands (no hidden behavior)

### `tools/` MUST NOT contain
- anything requiring secrets embedded in source
- one-off personal scripts
- ad-hoc admin shortcuts that bypass RBAC
- production destructive tools without guardrails
- provider-specific automation that cannot be adapted

If a script cannot be reviewed safely, it does not belong here.

---

## Tool contract rules (non-negotiable)

Every tool MUST follow these rules:

1. **Idempotent by default**  
   Running a tool twice with the same inputs produces the same result and does not stack side effects.

2. **Safe-by-default**  
   Destructive actions require an explicit `--apply` (or equivalent) flag.

3. **Dry-run support**  
   If a tool can mutate state, it MUST support `--dry-run` and print intended actions without performing them.

4. **Deterministic output**  
   Output MUST be stable: sorted keys, stable ordering, normalized line endings.

5. **Explicit inputs**  
   Tools MUST accept explicit flags for environment, tenant, project, and paths. No hidden inference.

6. **Fail fast, fail loud**  
   Missing prerequisites MUST produce clear errors and non-zero exit codes.

---

## Standard CLI interface conventions

### Command shape

Tools SHOULD follow a consistent entry pattern:

~~~text
chartly-tool <command> [flags]
~~~

**Ownership note (important):**  
`chartly-tool` represents the **eventual unified CLI surface** for Chartly.  
Until a single binary exists, individual scripts MAY be invoked directly, but they MUST follow the same flag names, exit codes, safety rules, and output conventions described here.

If a script cannot be wrapped behind this interface later, it does not belong in `tools/`.

### Script layout (until unified CLI exists)

~~~text
tools/<tool-name>/<tool-name>.ps1
tools/<tool-name>/<tool-name>.py
tools/<tool-name>/<tool-name>.sh
~~~

### Required flags (when relevant)
- `--env <dev|staging|prod>`
- `--tenant <id>`
- `--project <id>`
- `--path <path>`
- `--format <json|yaml|text>`
- `--dry-run`
- `--apply`

### Exit codes (contract)
- `0` success
- `1` general error / unknown failure
- `2` invalid arguments / usage error
- `3` precondition failed (missing prereqs)
- `4` validation failed (contract mismatch)
- `5` unsafe operation blocked (missing `--apply`, policy violation)

### Logging (contract)
- Logs MUST be structured where possible (JSON lines recommended)
- Always print a single-line summary at end:
  - `OK` / `FAILED`
  - reason code
  - duration
- Include correlation IDs when calling Chartly APIs (`X-Request-Id`)

---

## Mutability & environment safety (global rule)

Tools are **not production shortcuts**.

### Global invariant
- Tools MAY freely mutate **non-production** environments (`dev`, `staging`) when properly gated.
- Tools MUST NOT mutate `prod` unless:
  - explicitly authorized,
  - explicitly flagged (`--apply --env prod`),
  - and audited (audit event or logged approval reference).

If a tool cannot enforce this distinction, it MUST NOT support mutation at all.

---

## Security rules for tools

Tools are part of the security boundary.

### Hard rules
- **No secrets** in source code or committed config
- Secrets MUST be provided via environment or external secret stores (not defined here)
- Logs MUST redact:
  - tokens
  - Authorization headers
  - secret refs that could reveal sensitive names (policy-dependent)
- Tools MUST run with least privilege:
  - smallest RBAC scopes
  - read-only defaults
- Tools MUST NOT disable TLS verification

### Output redaction (contract)
If a tool prints structured output:
- redact any field named:
  - `token`, `password`, `secret`, `authorization`, `private_key`
- do not print raw request headers by default

---

## Directory structure (recommended)

~~~text
tools/
  README.md
  bin/                      # wrappers/launchers (optional)
  profiles/
    resolve/                # profile resolver + hash printer
    lint/                   # schema validation + invariants
  examples/
    validate/               # validate example metadata + rules
  deployment/
    diff/                   # env diff + drift detection
    smoke/                  # readiness + version checks
  fixtures/
    load/                   # non-prod fixture loader (bounded)
  lib/                      # shared helpers (logging, hashing, flags)
  tests/                    # tool conformance tests
~~~

**Rule:** Anything under `lib/` must be imported by tools; no duplicated logic across scripts unless justified.

---

## Tool index (roadmap baseline)

| Tool | Status | Purpose | Mutates state | Dry-run |
|------|--------|---------|---------------|---------|
| profiles-lint | 🛠 | Validate profile schemas + invariants | No | n/a |
| profiles-resolve | 🛠 | Resolve profile refs + print resolved hash | No | n/a |
| examples-validate | 🛠 | Ensure examples follow governance rules | No | n/a |
| deploy-diff | 🛠 | Detect drift between declared and live state | No | n/a |
| deploy-smoke | 🛠 | Smoke test health/readiness/version | No | n/a |
| fixtures-load | 🛠 | Load bounded fixtures into non-prod datasets | Yes | Yes |

---

## Safety patterns (required)

### Apply gating (example)
- default behavior: plan only
- `--apply`: perform side effects
- tools MUST print a confirmation summary before applying actions

### Bounded fixtures rule
Fixture tools MUST require:
- explicit `--env dev` (or non-prod)
- explicit `--tenant` and `--project`
- explicit `--dataset`
- explicit `--window_start` and `--window_end`
- a hard cap on loaded records (configurable)

---

## Documentation & tests

### Documentation requirement
Every tool directory MUST include:
- `README.md` with usage examples
- flags table
- exit codes
- safety notes

### Conformance tests (minimum)
A tool is not  without:
- help output test
- invalid flag test (exit code `2`)
- deterministic output test (stable ordering)
- `--dry-run` vs `--apply` behavior test (when mutating)

---

## Next steps (🛠)

- Implement `profiles-lint` and `profiles-resolve` first (they unlock  promotion across docs)
- Add `examples-validate` to enforce the evidence doctrine
- Add `deploy-diff` to make no drift enforceable
- Wire tool conformance tests into CI
