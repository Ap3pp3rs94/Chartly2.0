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

### 3) Start drones
```powershell
.\scripts\start-drone.ps1 -ControlPlane http://localhost:8090
```

### 4) Watch logs
```powershell
.\scripts\logs.ps1
.\scripts\logs.ps1 gateway
```

### 5) Verify
```powershell
Invoke-RestMethod http://localhost:8090/health
Invoke-RestMethod http://localhost:8090/api/status
Invoke-RestMethod http://localhost:8090/api/results/summary
```

### 6) Stop everything
```powershell
.\scripts\stop-all.ps1
```

### 7) Reset data (destructive)
```powershell
# dry-run
.\scripts\reset-data.ps1

# apply
.\scripts\reset-data.ps1 -Scope control-plane -Apply
```

---

## Mac/Linux (bash)

First time:
```bash
cd /path/to/Chartly2.0
chmod +x scripts/*.sh
```

### 1) Doctor + up
```bash
./scripts/control-plane-doctor.sh
./scripts/control-plane-up.sh
```

### 2) Start drones
```bash
./scripts/drone-up.sh --control-plane http://localhost:8090 --interval 5m --count 1
```

### 3) Logs
```bash
./scripts/logs.sh
./scripts/logs.sh gateway
```

### 4) Verify
```bash
./scripts/verify-control-plane.sh http://localhost:8090
```

### 5) Stop everything
```bash
./scripts/stop-all.sh
```

### 6) Reset data (destructive)
```bash
# dry-run
./scripts/reset-data.sh

# apply
./scripts/reset-data.sh --scope control-plane --apply
```

---

## Related docs
- `docs/DEPLOYMENT.md`
- `docs/TROUBLESHOOTING.md`
- `docs/PROFILES.md`
- `docs/API.md`
