# Chartly 2.0  Kubernetes Manifests

This directory contains Kubernetes manifests for Chartly 2.0.

## Goals

- **Provider-neutral**: no AWS/GCP/Azure assumptions.
- **No secrets in-repo**: credentials and tokens are not committed here.
- **Deterministic**: apply/delete commands are stable and repeatable.
- **Contracts-first**: manifests define the runtime contract for core services.

## Namespace

All resources are scoped to the namespace:

- `chartly`

## Layout

Expected layout:

- `manifests/`
  - `base/`  minimal, provider-neutral manifests (required)
  - `overlays/`  optional later (dev/prod, ingress, storage class, etc.)

> `base/` is the only required layer. Add overlays only when a deployment target is chosen.

### Overlays Contract (future)

- Overlays may adjust environment-specific details (replicas, resources, ingress, storage class, etc.).
- Overlays **must not** change the logical identity of core components (gateway/postgres/redis/qdrant).
- Keep names/selectors stable unless a breaking change is intentional and documented.

## Minimum Components (base)

The base layer is expected to define **at least**:

- **namespace** (Namespace)
- **gateway** (Deployment + Service)
- **postgres** (StatefulSet + Service)
- **redis** (Deployment + Service)
- **qdrant** (StatefulSet + Service)

## Storage Posture

- PVC usage is allowed for stateful services (Postgres, Qdrant).
- **StorageClass is not assumed** here.
  - If your cluster requires an explicit StorageClass, implement it via an overlay or cluster policy.
  - Base manifests should remain portable across clusters.

## Apply Order (deterministic)

Apply namespace first, then base resources:

    kubectl apply -f infra/k8s/manifests/base/namespace.yaml
    kubectl apply -f infra/k8s/manifests/base/

## Rollback / Delete (deterministic)

Delete base resources first, then namespace:

    kubectl delete -f infra/k8s/manifests/base/
    kubectl delete -f infra/k8s/manifests/base/namespace.yaml

> If PVCs are created, they may persist depending on your cluster reclaim policy.

## Notes

- This folder defines **runtime deployment manifests**, not application code.
- Service images/build pipelines are owned by the service directories and CI, not by this folder.
- Keep manifests minimal and portable; prefer overlays for environment-specific differences.
