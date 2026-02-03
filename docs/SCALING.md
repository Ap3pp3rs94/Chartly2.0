# Chartly 2.0  Scaling

## Contract status & trust model

This document defines **how Chartly scales**horizontally and verticallywithout violating determinism, cost controls, or operational safety.

### Legend
-  **Implemented**  verified in code and/or conformance tests
- 🛠 **Planned**  desired contract, may not exist yet
- 🧪 **Experimental**  available but may change without full deprecation guarantees

**Rule:** Anything not explicitly marked  is 🛠.

### Promotion criteria (🛠  )
A scaling mechanism becomes  only when:
- scaling triggers are explicit and measurable,
- behavior is bounded (no runaway scaling),
- and at least one load or stress test validates stability.

---

## Scaling philosophy

Chartly scales **by intent, not by accident**.

- Scaling decisions are **explicit**
- Scaling signals are **observable**
- Scaling actions are **bounded**
- Scaling costs are **predictable**

Scaling MUST never:
- change functional behavior
- introduce nondeterminism
- bypass safety or security invariants

---

## Dimensions of scaling

Chartly recognizes multiple, independent scaling dimensions.

| Dimension | What scales | Why |
|---------|-------------|-----|
| Compute | Pods / processes | Throughput, concurrency |
| Memory | Per-instance limits | Caching, buffering |
| I/O | Connections, streams | External ingest, storage |
| Data | Partitions, shards | Volume growth |
| Time | Batch windows | Load smoothing |

Scaling one dimension MUST NOT implicitly scale another.

---

## Control plane vs data plane scaling

### Control plane scaling

**Components**
- Gateway
- Orchestrator
- Auth
- Audit
- Observer

**Rules**
- Favor **stability over throughput**
- Scale conservatively
- Prefer vertical scaling first
- Protect with strict rate limits

**Anti-goals**
- Aggressive autoscaling
- Unbounded concurrency

---

### Data plane scaling

**Components**
- Connector Hub
- Normalizer
- Analytics
- Storage

**Rules**
- Favor **horizontal scaling**
- Partition work explicitly
- Isolate noisy neighbors
- Scale independently per service

---

## Horizontal scaling model

### Stateless services
Services MUST be stateless to scale horizontally.

- No in-memory durable state
- All state externalized (storage, checkpoints)
- Requests are idempotent where possible

### Partitioning
Work SHOULD be partitioned explicitly by:
- tenant
- project
- connector
- dataset
- time window

Partition keys MUST be stable.

---

## Autoscaling (🛠)

Autoscaling is **opt-in** and conservative by default.

### Allowed signals
- CPU utilization
- Memory utilization
- Queue depth
- In-flight request count
- Custom metrics (explicitly defined)

### Forbidden signals
- Error rate alone
- Latency percentiles alone
- External system signals without buffering

### Bounds
- Min replicas MUST be set
- Max replicas MUST be set
- Scale-up and scale-down cooldowns REQUIRED

### Scale-down safety (contract)
Scale-down SHOULD only occur when **all** are true for a sustained window:
- backlog is below steady-state threshold
- in-flight work is near zero
- error rate is within baseline
- no recent scale-up occurred

This prevents oscillation and premature downsizing.

---

## Backpressure & load shedding

Scaling is not the only defense.

### Backpressure
- Apply upstream rate limits
- Pause or slow connectors
- Reduce concurrency

### Load shedding
- Reject non-critical work
- Defer low-priority tasks
- Preserve core control-plane health

**Rule:** It is better to reject work than to destabilize the system.

---

## Queue-based scaling (🛠)

For bursty workloads, Chartly prefers queues.

### Queue properties
- Bounded depth
- Visibility timeouts
- Explicit retry semantics
- Dead-letter handling

### Scaling behavior
- Consumers scale with queue depth
- Producers are rate-limited
- Queue backlog is observable

---

## Storage & data growth scaling

### Storage growth rules
- Retention policies MUST be defined
- Partitioning MUST be explicit
- Compaction policies MUST be documented

### Multi-tenant isolation invariant
Storage growth for one tenant or project MUST NOT silently degrade:
- query latency
- write latency
- availability

for unrelated tenants or projects.

Isolation violations are contract failures.

### Read vs write scaling
- Writes optimized for throughput
- Reads optimized for query patterns
- Analytics workloads isolated from ingest paths

---

## Cost-aware scaling

Scaling decisions MUST consider cost.

### Cost guardrails
- Hard caps per environment
- Alerts on abnormal growth
- Budget-aware scaling limits

### Cost metric scope
- `cost_estimate` represents **approximate incremental cost**
- Reported at:
  - service level
  - environment level
- Intended for trend detection, not billing precision

### Anti-patterns
- Scaling on transient spikes
- Scaling without cost visibility
- Scale until it works behavior

---

## Failure modes & scaling safety

### Common failure scenarios
- Thundering herd on restart
- Runaway autoscaling loops
- External dependency saturation

### Safety controls
- Warm-up delays
- Progressive scale-up
- Dependency circuit breakers

---

## Observability for scaling

Scaling MUST be observable.

### Required metrics
- replica_count
- scale_events_total
- queue_depth
- backlog_duration
- cost_estimate

### Required logs
- scale decisions
- trigger signals
- bound enforcement

---

## Scaling invariants (hard rules)

- Scaling MUST NOT change correctness
- Scaling MUST NOT bypass security
- Scaling MUST be reversible
- Scaling MUST be explainable from metrics

If a scaling action cannot be explained post-hoc, it violates the contract.

---

## Operator checklist

Before enabling scaling:
- [ ] Statelessness verified
- [ ] Partition keys defined
- [ ] Autoscaling bounds set
- [ ] Scale-down safety window defined
- [ ] Backpressure paths tested
- [ ] Cost guardrails configured
- [ ] Observability dashboards live
- [ ] Failure scenarios rehearsed

---

## Next steps (🛠)

- Define reference autoscaling policies per service
- Add load-test fixtures for scale validation
- Integrate cost signals into scaling decisions
- Promote conservative autoscaling paths to 
