# Profiles

Profiles are **contracts** that tell drones:
1) where to fetch data (`source`)
2) how to normalize it (`mapping`)
3) how often to run (`schedule`, optional)

Profiles are designed to be deterministic, reviewable, and secret-free.

Profiles live under:
- `profiles/<domain>/*.yaml` (example: `profiles/government/*.yaml`)

---

## Minimal schema

```yaml
id: census-population
name: US Census Population Data
version: 1.0.0
description: State population data from 2020 Census
source:
  type: http_rest
  url: https://api.example.gov/data.json
  auth: none
schedule:
  enabled: true
  interval: 6h
  jitter: 30s
limits:
  max_records: 5000
  max_pages: 50
  max_bytes: 1048576
mapping:
  SOURCE_FIELD: dims.geo.name
  OTHER_FIELD: measures.population.total
```

### Field meanings
- `id`: stable identifier, unique across profiles
- `version`: bump when output changes
- `source.type`: currently `http_rest`
- `source.url`: full URL to fetch
- `source.auth`: currently `none` (no secrets in profiles)
- `schedule`: optional run frequency and jitter
- `limits`: caps for per-run safety
- `mapping`: source path to destination path

---

## Mapping semantics

### Destination paths
Destination values are dot paths that build nested objects.

Example:
```yaml
mapping:
  NAME: dims.geo.name
  P1_001N: measures.population.total
```

Output record:
```json
{
  "dims": { "geo": { "name": "California" } },
  "measures": { "population": { "total": "39538223" } }
}
```

### Source paths
Source keys represent a path in the input JSON:
- simple keys for flat objects (`NAME`)
- dot paths for nested objects (`company.name`)
- bracket indexes for arrays (`Results.series[0].data[0].value`)

Limitations:
- If a path does not exist, the mapped field is omitted.
- Arrays are accessed by explicit numeric indices only.

---

## Record IDs
Each output record is assigned a deterministic `record_id`:
- `record_id = sha256(canonical_record_json)`
- format: `sha256:<hex>`

This enables dedupe in the aggregator.

---

## Dims/Measures convention

To support reports and joins, profiles should output:
- `dims.*` for joinable keys (geo, time, entity)
- `measures.*` for numeric metrics

Example join keys:
- `dims.geo.state_code`
- `dims.time.year`

---

## Safety rules (hard)

- No secrets in profiles
- No tokens/passwords/keys
- Use public endpoints or inject secrets at runtime

---

## Testing profiles

Quick manual check:
```bash
curl -sS "<profile.source.url>" | head
```

End-to-end:
1) start control plane
2) start one drone
3) confirm `/api/results/summary` increases

---

## Example profile (government)

```yaml
id: bls-employment
name: Bureau of Labor Statistics Employment
version: 1.0.0
source:
  type: http_rest
  url: https://api.bls.gov/publicAPI/v2/timeseries/data/LNS14000000
  auth: none
schedule:
  enabled: true
  interval: 6h
  jitter: 30s
limits:
  max_records: 5000
  max_pages: 50
  max_bytes: 1048576
mapping:
  Results.series[0].seriesID: dims.series.id
  Results.series[0].data[0].year: dims.time.year
  Results.series[0].data[0].value: measures.employment.rate
```
