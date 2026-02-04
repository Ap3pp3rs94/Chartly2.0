# Deployment

This document covers how to deploy the **Chartly control plane + drones** in a governed, repeatable way.

Provider-neutral principle: the system should run on any Docker host and on any Kubernetes distribution. Cloud providers (AWS/GCP/Azure) are **options**, not assumptions.

---

## Components

**Control plane:**
- **gateway**: public entrypoint, reverse-proxy to internal services, optional UI hosting
- **registry**: profile storage/serving (YAML) for drones
- **aggregator**: results + records + runs (SQLite or Postgres)
- **coordinator**: drone registry, work queue, profile assignment
- **reporter**: ad-hoc reports (joins/correlation)

**Drones:**
- edge workers that fetch external sources and post normalized records to the control plane

---

## Deployment modes

### 1) Local / Dev (Docker Compose)
Best for:
- local validation
- demos
- integration work

Required files:
- `docker-compose.control.yml`
- `docker-compose.drone.yml`
- `profiles/government/*.yaml`

Windows:
```powershell
cd C:\Chartly2.0
.\scripts\control-plane-doctor.ps1
.\scripts\deploy-control.ps1
.\scripts\start-drone.ps1 -ControlPlane http://localhost:8090
```

Mac/Linux:
```bash
cd /path/to/Chartly2.0
chmod +x scripts/*.sh
./scripts/control-plane-doctor.sh
./scripts/control-plane-up.sh
./scripts/drone-up.sh --control-plane http://localhost:8090 --count 1
```

### 2) Single-host staging (Docker Compose)
Best for:
- a small team
- a single VM or bare-metal host

Recommendations:
- pin image tags (avoid `latest`)
- mount persistent volumes for `profiles/` and `data/`
- expose only the gateway publicly
- put the gateway behind a TLS terminator (reverse proxy)

### 3) Kubernetes (Production-style)
Best for:
- HA control plane
- independent scaling of services and drones
- integration with centralized ingress/observability

Provider-neutral approach:
- Deploy each service as a Deployment + Service
- Use a PersistentVolumeClaim for aggregator persistence
- Use Ingress or Gateway API for the gateway

Cloud options:
- AWS: EKS
- GCP: GKE
- Azure: AKS
- Also valid: k3s, kind, OpenShift, on-prem Kubernetes

---

## Configuration

### Environment variables (common)

Gateway:
- `REGISTRY_URL` (default `http://registry:8081`)
- `AGGREGATOR_URL` (default `http://aggregator:8082`)
- `COORDINATOR_URL` (default `http://coordinator:8083`)
- `REPORTER_URL` (default `http://reporter:8084`)

Coordinator:
- `REGISTRY_URL` (default `http://registry:8081`)

Registry:
- `PROFILES_DIR` (default `/app/profiles/government`)

Aggregator:
- `DB_DRIVER` (`sqlite` or `postgres`)
- `DB_DSN` (Postgres connection string when `DB_DRIVER=postgres`)

Drones:
- `CONTROL_PLANE` (required)
- `DRONE_ID` (optional; generated if blank)
- `PROCESS_INTERVAL` (optional; default `5m`)

### Auth (optional)
Control-plane services can enforce auth with:
- `AUTH_REQUIRED=true`
- `AUTH_TENANT_REQUIRED=true` (enforces `X-Tenant-ID`)

Gateway supports:
- `AUTH_JWT_HS256_SECRET_FILE=/path/to/secret`
- `AUTH_API_KEYS_FILE=/path/to/api_keys.json`
- `AUTH_API_KEYS_TTL_SECONDS=30`

---

## Persistence

### Local Compose
- `./profiles` mounted into registry
- `./data/control-plane` mounted into aggregator

### Production recommendation
SQLite is fine for dev and demos. For production:
- use Postgres or another managed DB
- move profile delivery to GitOps and publish into the registry
- consider persistent report storage

---

## Security considerations

- No secrets in profiles or code
- Gate profile writes with API keys (starter), replace with real auth in production
- Enforce least privilege on drones and services
- Restrict egress for drones in production
- Use TLS at the gateway/ingress

---

## Scaling guidance

- Drones scale horizontally
- Coordinator is stateful in-memory (production should externalize state)
- Aggregator becomes the bottleneck (move to Postgres before scaling)

---

## Operational checklist

- gateway `/health` returns healthy or degraded
- registry reports profiles loaded
- drones register and heartbeat
- results are arriving in aggregator
- data volume persists across restarts
