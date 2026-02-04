# Profiles

Profiles are **contracts** that tell drones:
1) where to fetch data (`source`), and  
2) how to normalize it (`mapping`).

Profiles are designed to be:
- deterministic
- reviewable (diff-friendly)
- secret-free

Profiles live under:
- `profiles/<domain>/*.yaml` (example: `profiles/government/*.yaml`)

---

## What a profile is (and is not)

### A profile IS
- a YAML contract artifact
- an instruction set for a drone run
- a declarative mapping from source fields → normalized fields

### A profile is NOT
- a place to embed credentials
- a runtime script
- a deployment config

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
mapping:
  SOURCE_FIELD: destination.path
  OTHER_FIELD: other.destination
```

---

## Source contract

### `source.type`
Supported: `http_rest`

### `source.url`
Must be an absolute URL.

### `source.auth`
Must be `none` in the base example; credentials are not embedded in profiles.

---

## Mapping semantics

Mapping is **declarative**:

- **Left side (source)**: a path in the source JSON
- **Right side (destination)**: a dot-path in the normalized record

Supported source path syntax:
- dot notation: `a.b.c`
- array indexes: `data[0].value`

Destination path:
- dot notation only: `dims.geo.state_code`

---

## Canonical output shape

Every record returned by a drone includes:
- `record_id` (sha256 of canonical JSON)
- mapped fields (under `dims.*`, `measures.*`, or legacy keys)

If `dims.time.occurred_at` is provided, the processor derives:
- `dims.time.date` (YYYY-MM-DD)
- `dims.time.year`
- `dims.time.month`

---

## Numeric coercion

If a mapped destination begins with `measures.`:
- numeric strings are coerced to float64 when possible
- non-numeric strings remain as-is

---

## Example (government profile)

```yaml
id: census-population
name: US Census Population Data
version: 1.0.0
source:
  type: http_rest
  url: https://api.census.gov/data/2020/dec/pl?get=NAME,P1_001N&for=state:*
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
  NAME: dims.geo.name
  state: dims.geo.state_code
  P1_001N: measures.population.total
```

---

## Schedule and limits

Profiles may include:

```yaml
schedule:
  enabled: true
  interval: 6h
  jitter: 30s
limits:
  max_records: 5000
  max_pages: 50
  max_bytes: 1048576
```

The coordinator enforces schedule + pause/resume semantics using overrides.

---

## Limitations

- No embedded secrets
- HTTP-only sources in v1
- No dynamic scripts or transformations
- Mapping is one-to-one field mapping (no computed expressions in v1)

---

## Testing tips

- Use `/api/profiles` to verify parsing
- Use `/api/results/summary` to validate ingest
- Use `/api/records?profile_id=...` to inspect normalized records
