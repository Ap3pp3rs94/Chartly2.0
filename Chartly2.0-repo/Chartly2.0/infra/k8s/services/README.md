# Chartly 2.0  K8s Service Manifests

This folder contains Kubernetes runtime manifests for Chartly 2.0 services.

## Namespace

All resources here are intended to run in:

- `chartly`

## What belongs here

This folder holds **runtime** Kubernetes objects for Chartly services, typically:

- Deployments / StatefulSets
- Services
- ConfigMaps (non-secret)
- PodDisruptionBudgets (optional later)
- NetworkPolicies (optional later)

## What does NOT belong here

- No real secrets (tokens, passwords, keys).
- No cloud-provider specific resources (LB annotations, managed DB assumptions) in base manifests.
- No image build logic. Dockerfiles and build pipelines live with service code and CI.

## Authority boundary (source of truth)

- **Service directories** own:
  - image build instructions (Dockerfile, build scripts)
  - application configuration defaults
- **This folder** owns:
  - Kubernetes runtime wiring (ports, selectors, probes, resources, volumes)
  - provider-neutral deployment contracts

## Naming + label contract (stable selectors)

All manifests should use stable names and selectors. Prefer:

- `metadata.name`: `chartly-<service>`
- `spec.selector.matchLabels` and Pod `metadata.labels` must match exactly

Minimum labels to include on all objects:

- `app.kubernetes.io/name: chartly`
- `app.kubernetes.io/part-of: chartly`
- `app.kubernetes.io/component: <gateway|postgres|redis|qdrant|...>`

## Deterministic apply/delete order

Recommended apply order:

    kubectl apply -f infra/k8s/namespaces.yaml
    kubectl apply -f infra/k8s/services/

Recommended delete order:

    kubectl delete -f infra/k8s/services/
    kubectl delete -f infra/k8s/namespaces.yaml
