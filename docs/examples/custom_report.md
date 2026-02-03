# Example  Custom Report

This example shows a **contract-driven** report request using the schemas in `contracts/v1/reports`.

## Steps

1) Build a `ChartConfig` object (see `chart_config.schema.json`).
2) Submit a report generation request (implementation-specific endpoint).
3) Expect a `ReportResult` object (see `report_result.schema.json`).

## Minimal Request Shape

```json
{
  "tenant_id": "tenant_a",
  "requested_at": "2026-01-01T00:00:00Z",
  "charts": [
    {
      "title": "Requests per minute",
      "chart_type": "line",
      "dataset": {
        "source_id": "gateway",
        "metric_name": "requests"
      },
      "time_range": {
        "start": "2025-12-01T00:00:00Z",
        "end": "2026-01-01T00:00:00Z",
        "granularity": "minute"
      }
    }
  ]
}
```
