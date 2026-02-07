# Troubleshooting

This guide covers common failure modes for the control plane + drones.

---

## Quick triage

Control plane containers:
```bash
docker compose -f docker-compose.control.yml ps
```

Tail logs (Windows):
```powershell
.\scripts\logs.ps1
.\scripts\logs.ps1 gateway
```

Tail logs (Mac/Linux):
```bash
./scripts/logs.sh
./scripts/logs.sh gateway
```

List drone containers:
```bash
docker ps --filter "name=chartly-drone-" --format "{{.Names}}"
```

---

## 1) Docker compose fails

Symptoms:
- `docker: command not found`
- `docker compose: not a docker command`

Fix:
- Install/upgrade Docker Desktop (Windows/Mac)
- Install Docker Engine + Compose v2 (Linux)

---

## 2) Ports already in use

Symptoms:
- compose fails to start
- gateway unreachable at `http://localhost:8090`

Fix:
- stop the conflicting service
- or change port mappings in `docker-compose.control.yml`

---

## 3) Gateway /health is degraded

Meaning: gateway is up, but one or more downstream services are unhealthy.

Fix:
- inspect `/api/status`
- check service logs

---

## 4) Profiles not loading

Symptoms:
- registry `/health` shows `profiles_count=0`
- drones register with zero profiles

Fix:
- confirm `profiles/government/*.yaml` exist
- verify volume mount `./profiles:/app/profiles`
- check registry logs

---

## 5) POST /profiles returns 403

Cause:
- `REGISTRY_API_KEY` not configured, or missing `X-API-Key`

Fix:
- set `REGISTRY_API_KEY` and include header in requests

---

## 6) Drone cannot reach control plane

Symptoms:
- connection refused or DNS errors in drone logs

Fix:
- if drone runs in Docker, use `http://host.docker.internal:8090`
- verify gateway port mapping in compose

---

## 7) Upstream APIs return 403 or 429

Public APIs often enforce quotas or strict user-agent policies.

Fix:
- increase interval (`PROCESS_INTERVAL`)
- reduce concurrent drones
- verify endpoint supports GET (some require POST)

---

## 8) Aggregator database errors

Symptoms:
- `database locked` (SQLite)
- write permission errors

Fix:
- ensure `./data/control-plane` is writable
- do not run multiple aggregator replicas with SQLite
- switch to Postgres for production

---

## 9) No results even with active drones

Checklist:
- drones registered? (`/api/drones`)
- profiles visible? (`/api/profiles`)
- upstream source returns JSON
- mapping matches returned JSON shape

Tip: start with a single known-good profile and one drone.
