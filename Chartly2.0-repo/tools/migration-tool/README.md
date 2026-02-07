# Chartly Tool  Migration Tool

## Contract status & trust model

**migration-tool** is a governed utility for executing deterministic migrations across Chartly environments and contract versions with strict safety gating.

### Legend
-  **Implemented**  tool exists and passes conformance tests
- 🛠 **Planned**  intended behavior, not yet implemented
- 🧪 **Experimental**  may change; do not rely on in automation

**Rule:** Anything not explicitly marked  is 🛠.

### Promotion criteria (🛠  )
migration-tool becomes  only when:
- plan output is deterministic and hash-stable,
- apply is idempotent and safe-by-default,
- rollback plans are generated and verifiable,
- and conformance tests validate exit codes and safety gates.

---

## What migration-tool is (and is not)

### migration-tool IS
- a deterministic planner and executor for migrations
- idempotent by default (re-runs do not stack side effects)
- safe-by-default (plan-only unless `--apply`)
- auditable (plans, hashes, and outcomes recorded)
- scoped and explicit (env/tenant/project/context must be provided)

### migration-tool IS NOT
- a build system
- a secret management tool
- a provider-specific infra tool
- a run anything admin shell

If a migration cannot be reviewed safely, it does not belong here.

---

## Why this tool exists

Deployments and contracts evolve. migration-tool prevents chaos by providing:

- deterministic migration plans
- explicit step execution ordering
- safe application and rollback guidance
- audit evidence of what changed and why

---

## Safety model (non-negotiable)

### Modes (explicit)

#### `plan` (default)
- always offline where possible
- no writes
- produces a deterministic plan + plan hash

#### `apply`
- performs changes described by the plan
- REQUIRES `--apply`
- idempotent steps only
- emits outcome report + hashes

#### `verify`
- always read-only
- cannot repair drift
- verifies expected state matches the plan

### Production protection
- apply in `prod` is forbidden by default
- enabling prod requires:
  - explicit `--env prod`
  - explicit `--apply`
  - explicit `--prod-override <ticket-id>` (audit reference)

---

## Apply and verify semantics (tightened)

### Apply (contract)
- apply MUST use only:
  - an explicit plan file (`--plan`), OR
  - explicit inputs (`--migration`, `--target-version`, scope flags)
- apply MUST NOT:
  - fetch or resolve `latest`
  - auto-discover a target version
  - pull remote state to decide what to do beyond the declared plan
- apply SHOULD minimize external dependencies and fail fast if required services are unavailable.

### Verify (contract)
- verify is always read-only
- verify MUST NOT mutate state, even to fix drift
- verify MUST report:
  - drift details
  - recommended rollback reference (if available)

---

## Determinism contract

Given the same:
- migration id
- inputs (env/tenant/project)
- target version
- flags

migration-tool MUST produce:
- identical plan JSON bytes
- identical plan hash
- identical ordered step list

**Rule:** No current time in plans. All timestamps are runtime logs only.

---

## CLI interface

### Command shape
~~~text
chartly-tool migration-tool <plan|apply|verify> [flags]
~~~

### Required flags (roadmap)
- `--env <dev|staging|prod>`
- `--migration <id>`
- `--target-version <semver>`
- `--format <json|text>`

When scoped:
- `--tenant <id>`
- `--project <id>`

### Optional flags
- `--apply` (required for apply)
- `--dry-run` (apply mode: show actions only)
- `--prod-override <ticket-id>` (required for prod apply)
- `--plan <path>` (use an explicit plan file)
- `--out <path>` (write reports and hashes)

---

## Exit codes (contract)

- `0` success
- `1` general error
- `2` invalid arguments / usage error
- `3` precondition failed (missing prerequisites)
- `4` validation failed (drift, incompatible version, contract failure)
- `5` unsafe operation blocked (missing `--apply`, missing override, policy violation)

---

## Plan output schema (JSON, canonical)

Plan output MUST be stable and ordered.

### Top-level fields
- `header`
  - tool version
  - env
  - migration id
  - target version
  - scope (tenant/project optional)
- `steps` (ordered)
- `plan_hash` (sha256 over canonical JSON)
- `rollback` (ordered guidance)

### Step fields
- `index` (1-based)
- `step_id` (stable)
- `type` (taxonomy below)
- `description`
- `idempotency_key`
- `preconditions` (ordered list)
- `actions` (ordered list)
- `postconditions` (ordered list)

---

## Idempotency key scoping (binding)

`idempotency_key` for a step is scoped to:
- `env`
- `tenant` (if present)
- `project` (if present)
- `migration_id`
- `step_id`

This prevents collisions across environments and scopes and ensures re-runs are safe.

---

## Migration step taxonomy

| Type | Description | Examples |
|------|-------------|----------|
| `schema` | change schema/contracts | add field, bump schema version |
| `data` | migrate stored data | backfill derived columns |
| `config` | migrate runtime config | config map key rename |
| `profile` | profile version bump | switch workflow refs to new profile version |
| `index` | index maintenance | rebuild index |
| `cleanup` | safe cleanup | remove deprecated artifacts after cutover |

**Rule:** Steps must be independently idempotent.

---

## Rollback strategy (DEPLOYMENT-aligned)

Rollback MUST be deterministic and time-bounded.

### Expectations
- rollback SHOULD complete within minutes
- rollback MUST NOT require rebuilds
- rollback MUST preserve audit logs

### Rollback guidance
Plans MUST include:
- reverse dependency order
- previous version references
- explicit stop/disable levers (e.g., disable workflow, pause connectors)
- data safety notes (what is irreversible)

---

## Observability & audit

Apply mode SHOULD emit:
- plan hash
- step outcomes
- failures with classification
- rollback reference

Audit evidence SHOULD include:
- who initiated migration
- what changed (migration id + version)
- where (env/tenant/project)
- when (runtime log)

---

## Conformance tests required for 

- deterministic plan hash test
- safety gate test (apply without `--apply`  exit 5)
- prod override test (prod apply without ticket  exit 5)
- idempotency test (apply twice  no extra side effects)
- idempotency scoping test (keys differ across env/tenant/project)
- rollback plan structure test (reverse order present)
- verify drift test (expected vs actual mismatch  exit 4)
- exit code contract tests (2/3/4/5)

---

## Next steps (🛠)

- Implement `plan` first with canonical JSON + plan hash
- Implement `verify` second (read-only drift detection)
- Implement `apply` last with strict safety gating and idempotency checks
- Add one golden plan fixture for a profile version bump migration
