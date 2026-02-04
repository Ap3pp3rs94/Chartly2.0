# Chartly Tool  Profiler

## Contract status & trust model

The **profiler** is a governed tool for measuring and explaining **runtime performance characteristics** of Chartly services, workflows, and operations.

### Legend
-  **Implemented**  tool exists and passes conformance tests
- 🛠 **Planned**  intended behavior, not yet implemented
- 🧪 **Experimental**  may change; do not rely on for automation

**Rule:** Anything not explicitly marked  is 🛠.

---

## What the profiler is (and is not)

### The profiler IS
- a **read-only analysis tool**
- deterministic for a fixed input window and target
- safe-by-default (no mutation)
- explainable (metrics  conclusion)
- suitable for CI, staging, and production diagnostics (read-only)

### The profiler IS NOT
- a load generator
- a chaos or fault-injection tool
- a live debugger with shell access
- a replacement for observability systems

If a use case requires mutation, it does not belong here.

---

## Primary use cases

The profiler exists to answer **why something is slow or expensive**, not just *that* it is slow.

Supported questions:
- Which step in a workflow dominates runtime?
- Is the bottleneck CPU, memory, I/O, or waiting on dependencies?
- Are scaling limits being hit or respected?
- Is backpressure or throttling occurring?
- Did performance regress between two versions?

---

## Deterministic profiling contract

Profiling MUST be deterministic and explainable.

### Determinism rules
Given the same:
- target (service / workflow / run id)
- environment
- explicit time window
- profiler version

the profiler MUST produce:
- identical summaries
- identical ordering
- identical numeric aggregates (within defined precision)

### Non-goals
- microsecond-level instruction tracing
- non-repeatable sampling artifacts

---

## Targets supported (🛠)

The profiler MAY target one of the following:

- **Service**
  - name (e.g., `chartly-analytics`)
  - environment + namespace
- **Workflow**
  - workflow id or name
  - specific run id
- **Operation**
  - long-running operation id
- **Connector run**
  - connector id + run id

Targets MUST be explicit. No auto-discovery.

---

## CLI interface

### Command shape
~~~text
chartly-tool profiler <target> [flags]
~~~

Until the unified CLI exists, scripts MUST follow this interface:

~~~text
tools/profiler/profiler.ps1
tools/profiler/profiler.py
~~~

---

## Required flags

- `--env <dev|staging|prod>`
- `--target-type <service|workflow|run|connector>`
- `--target-id <id>`
- `--window-start <RFC3339>`
- `--window-end <RFC3339>`
- `--format <json|yaml|text>`
- `--dry-run`

### Optional flags
- `--compare-to <baseline-id>` (regression comparison)
- `--precision <ms|s>` (numeric rounding)
- `--include-metrics <list>` (explicit allowlist)

---

## Safety rules (hard)

- Profiler is **read-only**. No `--apply` flag exists.
- Profiler MUST NOT:
  - mutate state
  - scale services
  - trigger workflows
- Profiler MUST fail fast if:
  - window is missing
  - target is ambiguous
  - permissions are insufficient

Violations invalidate the tool.

---

## Output contract

Profiler output MUST be stable and structured.

### Output sections (required)
1. **Header**
   - tool version
   - target
   - environment
   - window
   - correlation id

2. **Summary**
   - total duration
   - dominant bottleneck (single label)
   - regression status (if comparison used)

3. **Breakdown**
   - ordered list of contributors:
     - step / component
     - duration
     - percentage of total

4. **Signals**
   - CPU utilization (avg/max)
   - memory utilization (avg/max)
   - I/O wait
   - throttling / backpressure indicators

5. **Findings**
   - human-readable conclusions
   - each conclusion MUST cite a metric

### Deterministic ordering
- Lists MUST be sorted by descending contribution
- Keys MUST be stable
- Numeric precision MUST be explicit

---

## Example output (semantic)

~~~json
{
  "summary": {
    "total_duration_ms": 8421,
    "bottleneck": "analytics_query",
    "regression": "none"
  },
  "breakdown": [
    { "component": "analytics_query", "duration_ms": 5120, "pct": 60.8 },
    { "component": "connector_sync", "duration_ms": 2310, "pct": 27.4 },
    { "component": "normalization", "duration_ms": 991, "pct": 11.8 }
  ]
}
~~~

---

## Regression comparison (🛠)

When `--compare-to` is supplied:

- both profiles MUST use identical windows
- output MUST include:
  - delta duration
  - delta percentage per component
- regression classification:
  - `improved`
  - `regressed`
  - `unchanged`

---

## Security & privacy

- Profiler MUST redact:
  - request payloads
  - tokens
  - identifiers beyond target id
- Profiler MUST NOT emit:
  - raw logs
  - stack traces
- Access is governed by RBAC:
  - read-only scopes only

---

## Exit codes

- `0` success
- `2` invalid arguments
- `3` precondition failed (missing data, window, target)
- `4` validation failed (contract mismatch)
- `5` access denied

---

## Operator checklist

Before relying on profiler output:
- [ ] Explicit window provided
- [ ] Target uniquely identified
- [ ] Profiler version recorded
- [ ] Output stored with incident or PR
- [ ] Findings traced to metrics

---

## Promotion to  criteria

The profiler may be marked  only when:
- deterministic output test exists
- regression comparison test exists
- permission denial test exists
- output schema stability test exists

---

## Next steps (🛠)

- Implement profiler data collectors
- Define canonical bottleneck taxonomy
- Add CI regression checks using profiler output
- Promote to  once conformance tests pass
