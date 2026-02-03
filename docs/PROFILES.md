# Profiles  (Chartly 2.0)

Profiles define **client connection defaults** and enforce safe invariants.
They align with `Profile.Validate()` in the unit tests.

## Fields

- `name` (required)  non-empty
- `tenant_id` (optional)  `[A-Za-z0-9._-]+`
- `base_url` (required)  `http://` or `https://`
- `default_headers` (optional)  lowercase keys only (`[a-z0-9-]+`)
- `timeouts.request_ms` (100..300000)
- `timeouts.connect_ms` (50..request_ms)

## Example

```yaml
profiles:
  - name: "default"
    tenant_id: "tenant_a"
    base_url: "http://localhost:8080"
    default_headers:
      x-request-id: "profile-req-001"
    timeouts:
      request_ms: 15000
      connect_ms: 1000
```
