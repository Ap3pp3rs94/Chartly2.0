# Run the Chartly Control Plane + Drones

This runbook starts the **control plane** (gateway + internal services) and one or more **drones**.

## Prereqs
- Docker Desktop / Docker Engine running
- `docker compose` available
- Public internet access (for public government API profiles)

---

## Base URL

By default, the gateway is reachable at:
- `http://localhost:8090`

If you changed your compose port mappings, use the mapped host port:
- `http://localhost:<port>`

---

## Windows (PowerShell)

### 1) Doctor check
```powershell
cd C:\Chartly2.0
.\scripts\control-plane-doctor.ps1
```

### 2) Start control plane
```powershell
.\scripts\deploy-control.ps1
```

### 3) Start a drone
```powershell
.\scripts\start-drone.ps1 -ControlPlane http://localhost:8090
```

### 4) Verify
```powershell
Invoke-RestMethod http://localhost:8090/health
Invoke-RestMethod http://localhost:8090/api/status
Invoke-RestMethod http://localhost:8090/api/results/summary
```

### 5) Logs
```powershell
# all control-plane logs
.\scripts\logs.ps1

# specific service (gateway|registry|aggregator|coordinator|reporter)
.\scripts\logs.ps1 -Service gateway
```

### 6) Stop
```powershell
.\scripts\stop-all.ps1
```

### 7) Reset data (dry-run default)
```powershell
.\scripts\reset-data.ps1

# destructive
.\scripts\reset-data.ps1 -Apply
```

---

## macOS / Linux (bash)

### 1) Doctor check
```bash
cd /path/to/Chartly2.0
./scripts/control-plane-doctor.sh
```

### 2) Bootstrap + start control plane
```bash
./scripts/control-plane-up.sh
```

### 3) Start one or more drones
```bash
./scripts/drone-up.sh --control-plane http://localhost:8090

# multiple drones
./scripts/drone-up.sh --control-plane http://localhost:8090 --count 3
```

### 4) Verify
```bash
curl -fsS http://localhost:8090/health
curl -fsS http://localhost:8090/api/status
curl -fsS http://localhost:8090/api/results/summary
```

### 5) Logs
```bash
# all control-plane logs
./scripts/logs.sh

# specific service
./scripts/logs.sh gateway

# drone logs
./scripts/logs.sh --drone chartly-drone-<id>
```

### 6) Stop
```bash
./scripts/stop-all.sh
```

### 7) Reset data (dry-run default)
```bash
./scripts/reset-data.sh

# destructive
./scripts/reset-data.sh --apply
```

---

## Common endpoints

- Health: `http://localhost:8090/health`
- Status: `http://localhost:8090/api/status`
- Profiles: `http://localhost:8090/api/profiles`
- Results summary: `http://localhost:8090/api/results/summary`
- Runs: `http://localhost:8090/api/runs`
- Records: `http://localhost:8090/api/records`
- Reports: `http://localhost:8090/api/reports`

---

## Notes

- If Docker is down, the doctor script will fail; start Docker and retry.
- If the UI is missing, build it separately (`web/dist`). APIs still work without UI.
- Profiles must exist under `profiles/government/*.yaml` (or your domain folder).
- The registry blocks POST unless `REGISTRY_API_KEY` is set and `X-API-Key` matches.
