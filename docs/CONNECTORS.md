# Connectors  (Chartly 2.0)

Connectors are integration adapters that ingest data into Chartly.
They follow a consistent lifecycle contract:

- **Open**: establish any external connections
- **Health**: report readiness or degraded state
- **Close**: gracefully disconnect

## Connector Hub

The Connector Hub service runs connectors and provides:

- standardized ingestion patterns
- shared retry + backoff policies
- consistent error envelopes

## Connector Contract (Logical)

A connector should expose:

- `Name()`
- `Open(ctx)`
- `Health(ctx)`
- `Close()`

Errors should be precise and deterministic. A closed connector must report
`errNotOpen` when `Health()` is invoked.

## Configuration

Connectors should read configuration from environment variables or config maps.
Secrets should be supplied via external secret managers, not hardcoded.
