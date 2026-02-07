# Chartly 2.0  Architecture
## Mission
Chartly is a **provider-neutral platform runtime** that turns data pipelines into an operable virtual data center:
- predictable service boundaries
- strong defaults
- composable infrastructure contracts
- first-class observability
## Principles
1. **Provider-neutral by default**
   No cloud assumptions in core manifests or contracts. Providers are injected via overlays/adapters.
2. **Contracts over coupling**
   Kubernetes + Terraform modules define **contracts** first (inputs/outputs, invariants). Implementations come later.
3. **Secure-by-default runtime**
   Non-root, seccomp `RuntimeDefault`, drop all Linux capabilities, no privilege escalation, read-only root filesystem, explicit writable mounts.
4. **Deterministic operability**
   Explicit labels/selectors, explicit rollout controls, stable outputs, predictable paths, and safe defaults.
5. **Observability is not optional**
   Metrics and dashboards are part of the base platform, not an afterthought.
## High-level System Diagram
             
                        Clients              
               UI / API Consumers / Tools    
             
                              HTTP
                      
                         Gateway       chartly-gateway
                       (Edge + Auth) 
                      
                              internal HTTP
    
                                                      
  
 Orchestrator   Connector Hub   Observer 
 workflow brain   ingress adapters   telemetry/watch 
  
  
  
  
 Normalizer   Analytics   Audit 
 transform/shape   compute/queries   immutable trail 
  
  

 
 
 Storage   Auth 
 timeseries/io   identity/rbac 
 
## Services Overview
- **Gateway** (`chartly-gateway`)
  Front door: routing, edge behavior, auth entrypoints, and request normalization.
- **Orchestrator** (`chartly-orchestrator`)
  Coordinates workflows, schedules, retries, and system-wide execution state.
- **Connector Hub** (`chartly-connector-hub`)
  Connects to external systems (APIs/feeds), ingests events, and standardizes inbound integration patterns.
- **Normalizer** (`chartly-normalizer`)
  Converts raw inbound data into normalized schemas and canonical event formats.
- **Analytics** (`chartly-analytics`)
  Runs aggregations, derived computations, and query APIs over normalized + stored data.
- **Storage** (`chartly-storage`)
  Durable reads/writes for time-series and derived datasets (provider-neutral interface).
- **Audit** (`chartly-audit`)
  Writes immutable audit events (who/what/when) across major system actions.
- **Auth** (`chartly-auth`)
  Identity, sessions, permissions, and RBAC. (Deployed as a service boundary.)
- **Observer** (`chartly-observer`)
  Watches the system, emits telemetry, and can run periodic health + consistency checks.
## Control Plane vs Data Plane
**Control plane** coordinates what happens:
- Orchestrator (workflow logic)
- Auth (identity + permissions)
- Audit (tamper-evident trail)
- Observer (health + telemetry)
**Data plane** moves/transforms data:
- Connector Hub (ingest)
- Normalizer (transform)
- Storage (persistence)
- Analytics (compute/query)
This separation makes it easier to:
- secure boundaries
- reason about failure domains
- scale components independently
## Infrastructure Contracts
### Kubernetes (raw manifests)
Raw manifests provide a portable baseline:
- Deployment + Service per component
- stable labels/selectors (`app.kubernetes.io/*`)
- probes (`/ready`, `/health`)
- modest resource requests/limits
- deterministic security posture
- explicit writable mounts (`/tmp`, and data dirs where needed)
### Helm (Chartly base chart)
The Helm chart provides:
- composable defaults (`values.yaml`)
- explicit `selectorLabels` per component (no routing collisions)
- ingress routes (path-based) with deterministic `pathType`
- configmaps for non-secret config + monitoring provisioning
- optional HPA template (opt-in per component)
### Terraform (contract modules)
Terraform modules define **contracts** before provider implementations:
- `modules/vpc`  network contract + `vpc_spec`
- `modules/eks`  cluster contract + `cluster_spec` (provider-neutral)
- `modules/rds`  DB contract + `db_spec` (provider-neutral)
- `modules/redis`  cache contract + `redis_spec`
- `modules/s3`  object storage contract + `bucket_spec`
Contracts expose stable outputs:
- `*_spec` canonical object
- `*_spec_versioned` includes `contract_version`
- placeholders (`null` ids, `[]` lists) until implemented by provider overlays
## Security Baseline
The default posture aims to be secure enough to ship:
- `runAsNonRoot: true` with deterministic UID/GID
- `seccompProfile: RuntimeDefault`
- `allowPrivilegeEscalation: false`
- `capabilities.drop: ["ALL"]`
- `readOnlyRootFilesystem: true`
- explicit writable mounts (e.g., `/tmp`, and `/var/lib/grafana`, `/prometheus`)
- `automountServiceAccountToken: false` by default
- **No Secrets in Helm chart by default**
  Secrets are supplied via overlays or external secret managers.
## Observability
### Prometheus
- Scrapes `/metrics` from each service
- Base config is delivered via ConfigMap
- Runs provider-neutral (no Operator required)
### Grafana
- Base provisioning via ConfigMap (Prometheus datasource)
- Dashboards can be provisioned later via overlays
## Deployment Flows
### 1) Raw Kubernetes Manifests
Use `infra/k8s/**` manifests directly:
- good for learning and debugging
- low magic, high clarity
### 2) Helm
Use `infra/helm/chartly`:
- environment overrides via `values-*.yaml`
- deterministic selectors/labels
- ingress + config + autoscaling templates
### 3) Terraform (Contracts + Overlays)
Use `infra/terraform`:
- contracts define shapes and invariants
- provider overlays implement actual resources
- stable outputs feed into deployment automation
---
## Next Steps
- Add remaining service templates (all components + monitoring).
- Add dashboards provisioning (Grafana) in a non-secret ConfigMap.
- Add policy overlays (NetworkPolicies, PDBs) for production clusters.
- Add a gateway contract (Terraform/Helm) for ingress and edge routing.
