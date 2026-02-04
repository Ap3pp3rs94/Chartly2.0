# API

This document describes the **control-plane HTTP API** exposed by the gateway.
All routes are accessible under:

```
http://<gateway>/api
```

---

## Conventions

- JSON only
- Strict request decoding (unknown fields rejected)
- Deterministic ordering in list endpoints

---

## Health

### `GET /health`
Returns control-plane health.

Example:
```json
{"status":"healthy","services":{"registry":"up","aggregator":"up","coordinator":"up","reporter":"up"}}
```

---

## Profiles

### `GET /api/profiles`
List all profiles (sorted by id).

### `GET /api/profiles/{id}`
Fetch a single profile.

### `GET /api/profiles/{id}/status`
Returns latest run for that profile if aggregator is reachable.

### `POST /api/profiles`
Create a profile.

Requires header:
```
X-API-Key: <REGISTRY_API_KEY>
```

Body:
```json
{"id":"example","name":"Example","version":"1.0.0","content":"...yaml..."}
```

---

## Results

### `POST /api/results`
Store results from a drone run.

Body:
```json
{
  "drone_id":"drone-1",
  "profile_id":"census-population",
  "run_id":"uuid",
  "data":[ {"record_id":"sha256:...","dims":{},"measures":{}} ]
}
```

Response:
```json
{"inserted_results":10,"inserted_records":9,"deduped_records":1,"run_id":"uuid"}
```

### `GET /api/results/summary`
Summary across all results.

---

## Records

### `GET /api/records?profile_id=&run_id=&limit=100`
Returns canonical record JSON blobs.

---

## Runs

### `POST /api/runs`
Store a run record.

Body:
```json
{
  "run_id":"uuid",
  "drone_id":"drone-1",
  "profile_id":"census-population",
  "started_at":"2026-02-02T12:00:00Z",
  "finished_at":"2026-02-02T12:00:01Z",
  "status":"succeeded",
  "rows_out":123,
  "duration_ms":1000,
  "error":null
}
```

### `GET /api/runs?profile_id=&drone_id=&limit=100`
Returns run history (ordered by started_at desc).

### `GET /api/runs/{run_id}`
Fetch a single run.

---

## Drones

### `POST /api/drones/register`
Registers a drone and returns assigned profiles.

### `POST /api/drones/heartbeat`
Heartbeat and status update.

### `GET /api/drones`
List active drones.

### `GET /api/drones/stats`
Drone summary.

### `POST /api/profiles/{id}:runNow`
Forces a run on all drones (queued).

### `GET /api/drones/{id}/work`
Returns queued forced profile list for that drone.

---

## Reports

### `POST /api/reports`
Builds a joined report from records.

Example request:
```json
{
  "join":["dims.geo.county_fips","dims.time.year"],
  "inputs":[
    {"profile_id":"census-population","measure":"measures.population.total"},
    {"profile_id":"bls-employment","measure":"measures.employment.rate"}
  ],
  "window":{"limit":5000},
  "output":{"type":"correlation","method":"pearson"}
}
```

Example response:
```json
{"report_id":"sha256:...","created_at":null,"spec_hash":"sha256:...","result":{"correlation":0.72,"row_count":120}}
```

---

## Mapping limitations

- mapping is path-to-path only (no computed expressions)
- no secrets in profiles
- numeric coercion only for `measures.*`

---

## Error shape (common)

```json
{"error":"not_found"}
```

---

## Notes

- All timestamps are RFC3339 UTC.
- List endpoints are ordered for deterministic output.
