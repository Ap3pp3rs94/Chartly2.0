# Deployment  (Chartly 2.0)

Chartly supports multiple deployment flows, all provider-neutral by default.

## 1) Local Dev (Docker Compose)

Canonical dev compose lives here:

- `infra/docker/docker-compose.dev.yml`

You can still use the repo-root `docker-compose.yml` during migration.

## 2) Raw Kubernetes Manifests

Apply manifests in `infra/k8s/` directly:

- `infra/k8s/namespaces.yaml`
- `infra/k8s/services/*.yaml`
- `infra/k8s/ingress/ingress.yaml`

## 3) Helm Chart

Use the Chartly Helm chart:

- `infra/helm/chartly/`

Override values via `values-<env>.yaml` files.

## 4) Terraform (Contracts + Overlays)

Terraform is provider-neutral at the contract layer:

- `infra/terraform/modules/*`

Provider overlays are expected to implement the resources.

## Safety Gates

- No secrets in repo
- Explicit env selection for deploy scripts
- Production requires extra confirmation flags
