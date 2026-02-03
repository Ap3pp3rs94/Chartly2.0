# Example  ML Projection

This example uses the `projection_request.schema.json` contract.

## Minimal Request Shape

```json
{
  "tenant_id": "tenant_a",
  "requested_at": "2026-01-01T00:00:00Z",
  "model": { "name": "baseline_forecast" },
  "input": {
    "source_id": "analytics",
    "metric_name": "events_per_minute",
    "time_range": {
      "start": "2025-12-01T00:00:00Z",
      "end": "2026-01-01T00:00:00Z",
      "granularity": "minute"
    }
  },
  "horizon": { "steps": 1440, "granularity": "minute" }
}
```

This shape is provider-neutral. Execution is handled by the platform implementation.
