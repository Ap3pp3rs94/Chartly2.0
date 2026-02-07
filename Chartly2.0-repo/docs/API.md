# API

This document describes the control-plane API surface.

Base URL (local):
- `http://localhost:8090`

---

## Health

### Gateway
`GET /health`

Response:
```json
{
  "status": "healthy",
  "services": {
    "registry": "up",
    "aggregator": "up",
    "coordinator": "up",
    "reporter": "up"
  }
}
```

### Status
`GET /api/status`

---

## Profiles (registry via gateway)

### List
`GET /api/profiles`

### Get one
`GET /api/profiles/{id}`

### Create (governed write)
`POST /api/profiles`

Headers:
- `X-API-Key: <value>` (starter builds)

Body:
```json
{
  "id": "example",
  "name": "Example",
  "version": "1.0.0",
  "content": "yaml..."
}
```

---

## Results (aggregator via gateway)

### Insert results
`POST /api/results`

Body:
```json
{
  "drone_id": "drone-123",
  "profile_id": "census-population",
  "run_id": "run-123",
  "data": [
    {"dims":{"geo":{"name":"CA"}},"measures":{"population":{"total":39538223}}}
  ]
}
```

### Query results
`GET /api/results?drone_id=&profile_id=&limit=100`

### Summary
`GET /api/results/summary`

---

## Records (deduped)

`GET /api/records?profile_id=&run_id=&limit=100`

---

## Runs

### Upsert run
`POST /api/runs`

Body:
```json
{
  "run_id": "run-123",
  "drone_id": "drone-123",
  "profile_id": "census-population",
  "started_at": "2026-02-04T12:00:00Z",
  "finished_at": "2026-02-04T12:00:10Z",
  "status": "succeeded",
  "rows_out": 123,
  "duration_ms": 10000,
  "error": ""
}
```

### List runs
`GET /api/runs?drone_id=&profile_id=&limit=100`

### Get run
`GET /api/runs/{run_id}`

---

## Drones (coordinator)

### Register
`POST /api/drones/register`

Body:
```json
{ "id": "drone-123" }
```

### Heartbeat
`POST /api/drones/heartbeat`

### List active drones
`GET /api/drones`

### Stats
`GET /api/drones/stats`

### Work queue
`GET /api/drones/{id}/work`

---

## Reports

`POST /api/reports`

Body:
```json
{
  "join": ["dims.geo.state_code","dims.time.year"],
  "inputs": [
    {"profile_id":"census-population","measure":"measures.population.total"},
    {"profile_id":"bls-employment","measure":"measures.employment.rate"}
  ],
  "window": {"limit": 5000},
  "output": {"type": "correlation", "method": "pearson"}
}
```

---

## Errors

Recommended conventions:
- `400` invalid JSON / invalid parameters
- `403` missing or invalid auth
- `404` resource not found
- `409` conflict
- `429` rate limited (planned)
- `500` internal error
