# Deployment

This document covers how to deploy the **Chartly hybrid control-plane + drone** architecture in a governed, repeatable way.

> Provider-neutral principle: the system should run on any Docker host and on any Kubernetes distribution.
> Where cloud providers are mentioned (AWS/GCP/Azure), they are **options**, not assumptions.

---

## Components

**Control plane (Docker Compose example):**
- **gateway**: public entrypoint, reverse-proxies internal services and serves the UI (if built)
- **registry**: stores/serves profiles (YAML) for drones to consume
- **aggregator**: stores results (SQLite in the starter implementation)
- **coordinator**: tracks drones and assigns profiles
- **reporter**: builds reports by joining records (table + correlation)

**Drones:**
- edge workers that periodically fetch sources described by profiles and post normalized results back to the control plane.

---

## Deployment modes

### 1) Local / Dev (Docker Compose)
Best for:
- local validation
- demos
- initial integration work

Expected files:
- `docker-compose.control.yml`
- `docker-compose.drone.yml`
- `profiles/government/*.yaml`

Run (Windows PowerShell):
```powershell
cd C:\Chartly2.0
.\scripts\deploy-control.ps1
.\scripts\start-drone.ps1 -ControlPlane http://localhost:8090
```

Run (macOS/Linux bash):
```bash
cd /path/to/Chartly2.0
./scripts/control-plane-up.sh
./scripts/drone-up.sh --control-plane http://localhost:8090
```

Verify:
- `http://localhost:8090/health`
- `http://localhost:8090/api/status`
- `http://localhost:8090/api/profiles`
- `http://localhost:8090/api/results/summary`

Stop:
```powershell
.\scripts\stop-all.ps1
```
```bash
./scripts/stop-all.sh
```

Reset data (dry-run by default):
```powershell
.\scripts\reset-data.ps1
```
```bash
./scripts/reset-data.sh
```

---

### 2) Single Host (Docker Engine)
Best for:
- on‑prem single-node deployments
- labs
- edge gateways

Guidance:
- Use the compose files directly or convert them into systemd services.
- Keep `/app/data` mounted on persistent storage.

---

### 3) Kubernetes (recommended for production)
Best for:
- HA control plane
- rolling upgrades
- multi-tenant environments

Recommended approach:
- use Helm charts (`infra/helm/chartly`) for the platform
- use `values-<env>.yaml` per environment
- use per-namespace installs

Kubernetes runtime options (provider-neutral):
- managed Kubernetes (AWS EKS / GCP GKE / Azure AKS) — optional
- self-managed Kubernetes (k3s, kubeadm, OpenShift) — optional

---

## Environment layers

A minimal layering model:

1. **Base chart / manifests** (provider-neutral defaults)
2. **Environment overlays** (`dev`, `staging`, `prod`)
3. **Project overlays** (tenant-specific)

---

## Ports (defaults)

Control plane (Docker example):
- gateway: `8090`
- registry: `8091`
- aggregator: `8092`
- coordinator: `8093`
- reporter: `8094`

---

## Upgrade strategy

Safe upgrades require:
- immutable images
- deterministic config
- schema evolution discipline

Recommended flow:
1. Build + tag immutable images
2. Deploy to `dev`
3. Promote to `staging`
4. Promote to `prod`

---

## Data retention

- Results are stored in SQLite by default.
- In production, migrate to a managed database or a persistent volume.
- Back up `/app/data/results.db` regularly.

---

## Security baseline

- Control plane services run as non-root.
- No secrets embedded in the chart or docs.
- API write endpoints are gated via `X-API-Key` + `REGISTRY_API_KEY`.

---

## Next steps

- Add Helm-based production manifests
- Add CI to validate compose + profiles
- Add multi-node drone scheduling
