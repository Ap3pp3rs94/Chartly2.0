# Example  Onboard a New Domain

This guide outlines a minimal onboarding flow for a new data domain.

## Steps

1) Define contract schemas under `contracts/v1/<domain>/`.
2) Implement ingestion via Connector Hub (new connector or adapter).
3) Normalize raw events (Normalizer service).
4) Store normalized data (Storage service).
5) Expose queries and reports (Analytics service).
6) Add health checks (`/health`, `/ready`).

## Notes

- Keep schema changes backward-compatible.
- Add tests under `tests/` and scripts in `scripts/testing`.
