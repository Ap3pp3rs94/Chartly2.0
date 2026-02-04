# Chartly Profiler  Validators

## Contract status & trust model

This directory defines the **validator layer** for the Chartly profiler: deterministic checks that turn observed metrics snapshots into **contract-meaningful results** (pass/fail, findings, and enforcement outcomes).

### Legend
-  **Implemented**  validator exists and passes conformance tests
- 🛠 **Planned**  intended validator or behavior, not yet implemented
- 🧪 **Experimental**  may change; do not rely on in automation

**Rule:** Anything not explicitly marked  is 🛠.

---

## What validators are (and are not)

### Validators ARE
- deterministic checks over profiler inputs and observed snapshots
- **pure functions**: no network calls, no mutation, no time-based logic
- strict about platform contracts (tools contract, scaling invariants, security invariants)
- reusable across targets (service/workflow/run) when applicable
- explainable: every failure cites the rule and the signal that triggered it

### Validators are NOT
- collectors (they do not fetch metrics)
- heuristics engines with hidden randomness
- policy bypasses
- ad-hoc scripts with side effects

If a check needs network access, it belongs in a collector, not a validator.

---

## Validator contract (binding)

A validator MUST accept explicit inputs and produce deterministic outputs.

### Determinism rule
Given the same:
- validator version
- validator config
- profiler target + explicit time window
- observed snapshot

the validator MUST produce identical:
- pass/fail result
- finding list (stable ordering)
- result codes

### Ordering rule (stable output)
Findings MUST be sorted deterministically:
1. severity (desc: error  warn  info)
2. rule_id (asc)
3. component (asc)
4. metric (asc)

No map iteration order may leak into output.

---

## Numeric stability contract (binding)

Validators MUST avoid float flake and undefined numeric behavior.

### Rules
- Observed numeric values MUST be finite:
  - NaN/Inf are forbidden and MUST trigger `precondition_failed`
- Validators MUST NOT depend on float string formatting for correctness.
- Threshold comparisons MUST use explicit, stable rounding rules.

### Recommended representation
- Prefer integers when practical:
  - counts as `int64`
  - durations as `int64` milliseconds
  - percentages as basis points (`int64`, 10000 = 100.00%)
- If floats are unavoidable, validators MUST:
  - round inputs to a fixed precision before comparing
  - record the rounded value in the finding output

---

## Standard validator interface (Go)

Validators should be implemented behind a simple interface to keep the profiler deterministic and testable.

~~~go
package validators

type Severity string

const (
  SevInfo  Severity = "info"
  SevWarn  Severity = "warn"
  SevError Severity = "error"
)

type Finding struct {
  RuleID    string   `json:"rule_id"`
  Severity  Severity `json:"severity"`
  Component string   `json:"component"`
  Message   string   `json:"message"`
  Metric    string   `json:"metric,omitempty"`
  Value     string   `json:"value,omitempty"`
}

type Input struct {
  Env         string `json:"env"`
  TargetType  string `json:"target_type"`
  TargetID    string `json:"target_id"`
  WindowStart string `json:"window_start"`
  WindowEnd   string `json:"window_end"`

  // Observed is a pre-collected snapshot (no network calls here).
  // Values MUST be finite; NaN/Inf are invalid.
  Observed map[string]float64 `json:"observed"`

  // Optional: component-specific metrics (already collected).
  ObservedByComponent map[string]map[string]float64 `json:"observed_by_component,omitempty"`
}

type ResultCode string

const (
  CodeOK               ResultCode = "ok"
  CodeValidationFailed ResultCode = "validation_failed"
  CodePreconditionFail ResultCode = "precondition_failed"
  CodeNotApplicable    ResultCode = "not_applicable"
)

type Result struct {
  Ok       bool       `json:"ok"`
  Code     ResultCode `json:"code"`
  Findings []Finding  `json:"findings"`
}

type Validator interface {
  ID() string
  Run(in Input) Result
}
~~~

**Hard rule:** `Run` MUST NOT call time, randomness, the filesystem, or the network.

---

## Error taxonomy and exit-code mapping

Validators produce stable machine codes. The profiler aggregates them and maps outcomes to exit codes.

### Validator result codes (contract)
- `ok`  validator passed
- `validation_failed`  contract violation detected (deterministic failure)
- `precondition_failed`  missing/invalid required observed metrics for this validator
- `not_applicable`  validator does not apply to this target type (treated as ok)

### Profiler exit code mapping (roadmap, 🛠)
Validator aggregation is **roadmap**. When implemented, exit mapping MUST follow:

- If any validator returns `validation_failed`  profiler exits **4** (validation failed)
- If any validator returns `precondition_failed` and profiling cannot proceed  profiler exits **3** (precondition failed)
- Access denied remains **5**, argument errors remain **2**, internal errors remain **1**

**Rule:** Validators never return exit codes. They return only result codes and findings.

---

## Validator configuration (contract)

Validator configuration MUST be explicit and deterministic:
- no auto thresholds
- no learned baselines unless a fixed baseline artifact is provided
- all thresholds must be recorded in output (or referenced by immutable config id)

Recommended config patterns:
- `thresholds.<metric>` numeric values
- `components.allowlist` or `denylist`
- severity mapping per rule (warn vs error)

---

## Directory structure & naming rules

Recommended layout:

~~~text
tools/profiler/validators/
  README.md
  registry.go                 # validator registry (stable ordering by ID)
  types.go                    # shared types (Finding, Input, Result)
  operability/
    missing_metrics.go        # required-signal presence checks
    window_sanity.go          # basic window invariants (if needed)
  scaling/
    hpa_bounds.go             # min/max bounds sanity
    scale_flap.go             # oscillation detection
    queue_backlog.go          # sustained backlog growth
  security/
    egress_policy.go          # unexpected outbound patterns (boundary-only)
    webhook_replay.go         # replay signals (if present)
  cost/
    guardrails.go             # cost_estimate vs caps
~~~

### Naming rules
- File names MUST be consistent within a folder (snake_case or lowerCamelCase).
- Validator IDs MUST be stable and globally unique:
  - `operability.missing_metrics`
  - `scaling.hpa_bounds`
  - `security.egress_policy`
- IDs MUST NOT be renamed once released without a deprecation window.

### Registry rule (determinism)
Validator registration MUST be deterministic:
- registry MUST return validators in stable sorted order by `ID()`

---

## Planned validator index

| Validator ID | Status | Applies to | Required signals | Default threshold / rule | Failure code(s) |
|---|---:|---|---|---|---|
| operability.missing_metrics | 🛠 | all | per-target required set | fail if any required signal missing | precondition_failed |
| scaling.hpa_bounds | 🛠 | service | replica_count | min/max bounds must exist and min  max | validation_failed |
| scaling.scale_flap | 🛠 | service/workflow | scale_events_total, replica_count | fail if oscillation within window exceeds cap | validation_failed |
| scaling.queue_backlog | 🛠 | workflow/run | queue_depth, backlog_duration | fail if backlog grows for sustained window | validation_failed |
| security.egress_policy | 🛠 | connector/service | egress_denies_total, dns_allowlist_hits | fail if unexpected egress denies spike | validation_failed |
| cost.guardrails | 🛠 | env | cost_estimate | fail if cost exceeds configured cap | validation_failed |

**Notes**
- Validators MUST return `not_applicable` if the target type does not apply.
- Validators MUST return `precondition_failed` if required signals are missing or non-finite.

---

## Conformance tests required for 

A validator may be marked  only when tests exist for:

1. **Determinism**  
   Same input snapshot  identical output bytes (stable JSON encoding in test harness).

2. **Ordering**  
   Findings are sorted exactly by the ordering rule.

3. **Threshold behavior**  
   Boundary conditions behave correctly (just below/above threshold).

4. **Not applicable behavior**  
   Non-applicable targets return `not_applicable` deterministically with no findings.

5. **Precondition handling**  
   Missing required metrics return `precondition_failed` with an actionable finding.

6. **Float hygiene**  
   NaN/Inf or non-finite observed values return `precondition_failed` deterministically.

7. **Registry ordering** (for registry.go)  
   Registry returns validators sorted by `ID()`.

---

## Implementation notes

- Validators are contract enforcement, not heuristics.
- Keep thresholds conservative and explicit.
- Prefer explain why findings over silent pass/fail.
- Every finding should be actionable:
  - what to change
  - where to look
  - which signal proved it

---

## Next steps (🛠)

- Implement `types.go` and `registry.go` with stable ordering
- Implement `operability.missing_metrics` first (enables strict profiling)
- Add a JSON golden test for one validator and for the registry output
- Wire validators into profiler output under `findings` with stable ordering
