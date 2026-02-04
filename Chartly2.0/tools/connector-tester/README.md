# Chartly Tool  Connector Tester

## Contract status & trust model

The **connector-tester** is a governed tool that validates connector behavior against Chartlys contracts: determinism, paging correctness, retry classification, rate limiting, and security guardrails.

### Legend
-  **Implemented**  tool exists and passes conformance tests
- 🛠 **Planned**  intended behavior, not yet implemented
- 🧪 **Experimental**  may change; do not rely on in automation

**Rule:** Anything not explicitly marked  is 🛠.

### Promotion criteria (🛠  )
The tool becomes  only when:
- outputs are deterministic (golden test),
- it supports `plan`, `run`, and `validate` modes with strict semantics,
- it enforces non-prod mutation constraints,
- and conformance tests validate exit codes and stable output.

---

## What this tool is (and is not)

### connector-tester IS
- a deterministic harness for **connector contract validation**
- profile-driven (connector behavior comes from a resolved connector profile)
- safe-by-default (offline unless explicitly executing)
- suitable for CI, staging, and pre-prod verification

### connector-tester IS NOT
- a load test generator
- a production admin shortcut
- a place to embed credentials
- a bypass around connector security posture

---

## Safety model (non-negotiable)

### Execution modes (explicit)

The tool has **three distinct modes**. Their behavior is intentionally strict:

#### `plan` (always offline)
- **NEVER performs network calls**
- Resolves the connector profile
- Generates a deterministic request plan
- Computes a plan hash
- Safe in all environments

#### `run` (online, gated)
- Performs network calls
- **REQUIRES `--apply`**
- Enforced caps:
  - max pages
  - max records
  - max bytes
- Enforces SSRF + egress guardrails
- Mutates no persistent state unless explicitly allowed

#### `validate`
- Does not perform network calls
- Compares the computed plan against a golden plan artifact
- Produces deterministic validation findings

### `--dry-run` semantics (clarified)

- `--dry-run` **never enables network calls**
- `--dry-run` affects **output messaging only**
- Side effects occur **only** in `run` **with `--apply`**

**Rule:**  
If the tool performs network I/O, it MUST be in `run` mode **and** `--apply` MUST be set.

---

## Determinism contract

Given the same:
- connector profile ref + resolved hash
- env / tenant / project
- explicit `window_start` and `window_end`
- identical fixture inputs (if used)

The tool MUST produce identical:
- request plan (stable ordering)
- JSON output bytes
- hashes (profile hash, plan hash, optional response hash)

**Rule:** No relative now windows. All windows are explicit.

---

## CLI interface

### Command shape
~~~text
chartly-tool connector-tester <plan|run|validate> [flags]
~~~

Until a unified CLI exists:
~~~text
tools/connector-tester/connector-tester.ps1
tools/connector-tester/connector-tester.go
~~~

---

## Required flags

- `--env <dev|staging|prod>`
- `--tenant <id>`
- `--project <id>`
- `--connector-profile <namespace::connector/name@version[#variant]>`
- `--window-start <RFC3339>`
- `--window-end <RFC3339>`
- `--format <json|text>`

### Optional flags
- `--dry-run`
- `--apply` (required for `run`)
- `--max-pages <n>`
- `--max-records <n>`
- `--max-bytes <n>`
- `--golden <path>`

---

## Exit codes (contract)

- `0` success
- `1` general error
- `2` invalid arguments / usage error
- `3` precondition failed
- `4` validation failed
- `5` unsafe operation blocked / access denied

---

## Failure classification (explicit mapping)

| Scenario | Exit code | Rationale |
|-------|---------|-----------|
| Missing connector profile ref | 3 (precondition_failed) | Required input missing |
| Profile fails schema validation | 4 (validation_failed) | Contract violation |
| SSRF / egress blocked | 4 (validation_failed) | Security policy enforced |
| Rate limit exceeded | 4 (validation_failed) | Connector contract violation |
| 401 / auth failure from upstream | 4 (validation_failed) | Invalid auth configuration |
| Missing `--apply` in run | 5 (unsafe operation blocked) | Safety gate |
| Network attempt in plan | 5 (unsafe operation blocked) | Contract violation |

---

## Output contract (JSON is canonical)

### Required output fields
- `header`
  - tool version
  - env / tenant / project
  - connector profile ref
  - resolved profile hash
  - window bounds
  - mode (`plan|run|validate`)
  - dry_run / apply flags
- `plan`
  - pagination mode
  - ordered request steps
  - limits applied
  - plan hash
- `results` (run only; omitted until run is available)
- `validation`
  - ok / failed
  - findings (stable ordering)
  - code (`validation_failed` / `precondition_failed`)

### Stability guarantees
- stable key ordering
- fixed numeric precision
- no timestamps other than explicit window bounds

---

## Security posture (connector boundary)

The tool MUST enforce:
- no secrets printed
- secretRef only (no embedded auth)
- SSRF blocking (localhost, link-local, metadata)
- DNS allowlist enforcement
- TLS verification enabled
- hard caps on all ingestion paths

Violation of any guardrail  `validation_failed`.

---

## Example usage

### Plan (offline, safe everywhere)
~~~bash
chartly-tool connector-tester plan \
  --env dev --tenant tenant-demo --project proj-sales \
  --connector-profile "tenant-demo/proj-sales::connector/sales-http@1.0.0" \
  --window-start "2026-02-01T00:00:00Z" \
  --window-end "2026-02-02T00:00:00Z" \
  --format json
~~~

### Run in non-prod (explicit)
~~~bash
chartly-tool connector-tester run \
  --env dev --tenant tenant-demo --project proj-sales \
  --connector-profile "tenant-demo/proj-sales::connector/sales-http@1.0.0" \
  --window-start "2026-02-01T00:00:00Z" \
  --window-end "2026-02-02T00:00:00Z" \
  --max-pages 5 --max-records 1000 --max-bytes 1048576 \
  --apply
~~~

### Validate against golden
~~~bash
chartly-tool connector-tester validate \
  --env dev --tenant tenant-demo --project proj-sales \
  --connector-profile "tenant-demo/proj-sales::connector/sales-http@1.0.0" \
  --window-start "2026-02-01T00:00:00Z" \
  --window-end "2026-02-02T00:00:00Z" \
  --golden tools/connector-tester/golden/sales-http.plan.json
~~~

---

## Conformance tests required for 

- deterministic plan hash test
- pagination correctness test
- retry classification test
- SSRF / egress enforcement test
- unsafe mutation blocked test
- exit code contract tests
- output redaction test

---

## Next steps (🛠)

- Add golden plan fixtures
- Add `run` with strict caps and `--apply` (results section becomes active)
