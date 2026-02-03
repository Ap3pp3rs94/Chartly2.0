# Chartly 2.0  Deployment

## Contract status & trust model

This document defines **how Chartly is deployed and promoted** across environments in a way that is deterministic, auditable, and provider-neutral.

### Legend
-  **Implemented**  verified in code and/or conformance tests
- 🛠 **Planned**  desired contract, may not exist yet
- 🧪 **Experimental**  available but may change without full deprecation guarantees

**Rule:** Anything not explicitly marked  is 🛠.

### Promotion criteria (🛠  )
A deployment behavior becomes  only when:
- it is declaratively defined (manifests, charts, or plans),
- it is repeatable (same inputs  same result),
- and at least one dry-run or smoke test validates it.

---

## Deployment philosophy

Chartly follows **contract-first deployment**:

- infrastructure defines **where**
- manifests define **what**
- profiles define **behavior**
- workflows define **execution**

Each layer has a single responsibility. No layer may compensate for another.

---

## Supported deployment modes

Chartly supports multiple deployment mechanisms. These are **equivalent in intent**, not necessarily in ergonomics.

| Mode | Primary use | Characteristics |
|------|-------------|----------------|
| Raw manifests | Learning, debugging | Explicit, low magic |
| Helm charts | Standard environments | Parameterized, repeatable |
| Terraform + overlays | Full environments | Contract-driven infra + deploy |

---

## Environment model

Chartly environments are **explicit and isolated**.

### Canonical environments
- `dev`  rapid iteration, lowest guardrails
- `staging`  production-like, pre-release validation
- `prod`  locked-down, audited

### Environment invariants
- Separate namespaces / clusters / projects
- Separate profiles and overlays
- No shared secrets or mutable state
- Promotion is forward-only (`dev`  `staging`  `prod`)

---

## Control plane vs data plane deployment

Chartly components are deployed with strict plane separation.

### Control plane
- Gateway
- Orchestrator
- Auth
- Audit
- Observer

**Properties**
- Lower churn
- Higher security posture
- Strict RBAC
- Small blast radius

### Data plane
- Connector Hub
- Normalizer
- Analytics
- Storage

**Properties**
- Scales independently
- Higher throughput
- Tuned resource profiles
- Can be rolled independently

---

## Deployment security invariants (hard rules)

These invariants apply to **all environments** and **all services**:

- Containers MUST run as non-root
- Root filesystems MUST be read-only
- Writable paths MUST be explicitly declared
- Secrets MUST NOT appear in manifests, charts, or plans
- Network exposure MUST be explicit (no implicit ingress)
- Default-deny network posture is RECOMMENDED

Violations invalidate the deployment contract.

---

## Base deployment units

Each Chartly service is deployed as:

- Deployment (or equivalent)
- Service (stable selector)
- ConfigMap (non-secret config)
- Optional HPA
- Optional NetworkPolicy

These units MUST be independently deployable and rollback-safe.

---

## Raw Kubernetes deployment (🛠)

Raw manifests live under:
~~~text
infra/k8s/
  namespaces/
  services/
  monitoring/
~~~

### Characteristics
- Fully explicit YAML
- No templating
- Best for debugging and learning

### Apply semantics (clarified)
- In-place mutation is permitted **only** via reviewed manifest changes.
- Imperative patching (`kubectl edit`, ad-hoc `patch`) is forbidden.
- The applied state MUST always be derivable from version-controlled manifests.

### Apply flow
1. Create namespace
2. Apply base services
3. Apply monitoring
4. Verify readiness

### Rollback
- Re-apply the previous manifest version
- Or delete applied manifests in reverse order
- No partial or manual rollback steps

---

## Helm deployment (🛠)

Helm packages Chartly for repeatable installs.

### Chart structure
~~~text
infra/helm/chartly/
  Chart.yaml
  values.yaml
  templates/
    gateway.yaml
    orchestrator.yaml
    connector-hub.yaml
    ...
~~~

### Helm rules
- Values files are environment-specific
- Secrets are never in `values.yaml`
- Templates MUST be deterministic (no random or time-based functions)
- `helm template` output SHOULD semantically match raw manifests

### Promotion
- Same chart version
- Different values file
- No template changes during promotion

---

## Terraform deployment (🛠)

Terraform manages **infrastructure contracts**, not runtime behavior.

### Responsibility split
- Terraform manages:
  - networks
  - clusters
  - storage backends
  - base namespaces
- Helm / manifests manage:
  - Chartly services
  - runtime configuration

### Contract-first modules
- Modules expose stable inputs and outputs
- Providers are an implementation detail
- Outputs feed deployment automation without manual wiring

---

## Configuration layering

Configuration is layered intentionally.

~~~text
Base manifests / chart defaults
        
Environment overrides
        
Project overlays
        
Resolved runtime config (immutable)
~~~

**Rule:** Lower layers MAY override higher layers but MUST NOT remove required fields or weaken security invariants.

---

## Drift detection & reconciliation (🛠)

No drift is an enforceable rule.

### Drift detection expectations
- Periodic diff between:
  - declared manifests/charts
  - live cluster state
- Detection of:
  - unmanaged resources
  - manual edits
  - unexpected field mutations

### Response to drift
- Emit alert
- Record audit event
- Require reconciliation via declarative source

Manual correction without updating the source of truth is forbidden.

---

## Deployment safety mechanisms

### Readiness & health
- All services expose `/health` and `/ready`
- Rollouts MUST gate on readiness

### Progressive rollout
- Rolling updates by default
- Canary or blue/green MAY be layered later
- One change dimension per deploy (code OR config)

### Blast radius control
- Independent services
- Resource limits enforced
- Network policies where supported

---

## Promotion workflow (reference)

1. **Build**
   - Build immutable images
   - Tag with content hash / version

2. **Deploy to dev**
   - Apply manifests or Helm install
   - Run smoke tests

3. **Promote to staging**
   - Same artifacts
   - Different overlays
   - Run integration tests

4. **Promote to prod**
   - Approval required
   - Audit event emitted
   - No drift from staged artifacts

---

## Rollback strategy

Rollback MUST be deterministic and time-bounded.

### Expectations
- Rollback SHOULD complete within minutes
- Rollback MUST NOT require rebuilds
- Rollback MUST preserve audit logs

### Code rollback
- Redeploy previous image tag

### Config rollback
- Reapply previous config version
- Profiles referenced by version only (never mutable)

### Emergency rollback
- Disable ingress at Gateway
- Scale data-plane components to zero if required
- Preserve system state for postmortem

---

## Observability during deployment

Deployments MUST be observable.

### Signals to watch
- readiness failures
- error rate spikes
- latency regressions
- connector backpressure

### Required artifacts
- deployment logs
- version metadata
- request correlation IDs

---

## Audit & compliance

Every deployment SHOULD emit an audit event:
- who initiated it
- what changed (version, config)
- where it was deployed
- when it completed
- rollback reference (if any)

Audit logs are immutable and retained per policy.

---

## Operator checklist

Before promoting to production:
- [ ] Images immutable and versioned
- [ ] Manifests / charts validated
- [ ] No secrets in deployment artifacts
- [ ] Security invariants satisfied
- [ ] Readiness checks verified
- [ ] Resource limits set
- [ ] Drift detection enabled
- [ ] Rollback path tested
- [ ] Audit logging enabled

---

## Next steps (🛠)

- Add deployment conformance tests:
  - dry-run success
  - idempotent apply
  - rollback time-bound verification
- Add environment diff tooling
- Add deployment health dashboards
