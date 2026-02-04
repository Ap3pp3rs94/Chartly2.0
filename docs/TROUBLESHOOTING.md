# Troubleshooting

This document lists common issues and deterministic fixes for the Chartly control plane + drone system.

---

## Quick checks

### 1) Is Docker reachable?
```bash
docker info
```
If this fails, Docker Desktop (or engine) is not running.

### 2) Is the control plane healthy?
```bash
curl -fsS http://localhost:8090/health
```
If this fails, check service logs:
```bash
./scripts/logs.sh
```

### 3) Are profiles present?
```bash
curl -fsS http://localhost:8090/api/profiles
```
If empty, verify `profiles/government/*.yaml` exists.

---

## Common issues

### Docker is not reachable
**Symptom:** `docker info` fails

**Fix:**
- Start Docker Desktop (Windows/macOS) or the Docker daemon (Linux)
- Re-run `scripts/control-plane-doctor` for validation

---

### Port already in use
**Symptom:** `bind: address already in use`

**Fix:**
- Stop the conflicting service
- Or change the port mapping in `docker-compose.control.yml`

---

### Registry returns 403 on POST
**Symptom:** `{"error":"forbidden"}` or `{"error":"api_key_not_configured"}`

**Fix:**
- Ensure `REGISTRY_API_KEY` is set in the registry container
- Use `X-API-Key` header for POST requests

---

### Profiles list is empty
**Symptom:** `GET /api/profiles` returns `[]`

**Fix:**
- Ensure `profiles/government/*.yaml` exists on the host
- Ensure the registry container has a volume mount to `profiles/`

---

### Results not appearing
**Symptom:** `GET /api/results/summary` shows `total_results = 0`

**Fix:**
- Verify drone is running: `GET /api/drones`
- Check drone logs: `./scripts/logs.sh --drone <project>`
- Check aggregator logs: `./scripts/logs.sh aggregator`

---

### Reporter returns empty output
**Symptom:** `/api/reports` returns empty result table

**Fix:**
- Ensure records exist for the input profile_id
- Check `/api/records?profile_id=...&limit=10`

---

### Coordinator shows no active drones
**Symptom:** `/api/drones/stats` shows `active=0`

**Fix:**
- Ensure drone process is running
- Verify heartbeat endpoint: `POST /api/drones/heartbeat`

---

### Health shows degraded
**Symptom:** `/health` or `/api/status` returns `degraded`

**Fix:**
- Identify which service is down in `/api/status`
- Restart that service: `docker compose -f docker-compose.control.yml up -d <service>`

---

## Reset and recovery

### Reset data (dry-run default)
```bash
./scripts/reset-data.sh
```

### Reset data (destructive)
```bash
./scripts/reset-data.sh --apply
```

---

## Support artifacts

When filing an issue or requesting help, include:
- output of `./scripts/control-plane-doctor.sh`
- recent logs: `./scripts/logs.sh --no-follow --tail 200`
- `/api/status` response
