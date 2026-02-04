# Chartly 2.0

[![CI](https://github.com/Ap3pp3rs94/Chartly2.0/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Ap3pp3rs94/Chartly2.0/actions/workflows/ci.yml) [![Docker Build](https://github.com/Ap3pp3rs94/Chartly2.0/actions/workflows/docker-build.yml/badge.svg?branch=main)](https://github.com/Ap3pp3rs94/Chartly2.0/actions/workflows/docker-build.yml) [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Chartly 2.0 is a contracts-first data ingestion, normalization, and analytics platform designed to turn heterogeneous inputs into validated canonical records and publish charts, reports, and projections with a complete audit trail. This repository contains the full structure and incremental implementations; remaining work is tracked via roadmap markers and scaffolding notes.

## Core concepts
- Contracts-first: JSON Schemas in `contracts/` define canonical entities, events, metrics, and reports that every service must validate against.
- Profiles-over-code: YAML profiles in `profiles/` describe mappings, rules, cleansing, and retention so domain behavior is declarative.
- Auditability: All critical mutations are intended to be append-only and verifiable via the audit service.
- Quarantine: Invalid or unsafe records are isolated with reason codes and recovery workflows.
- Backpressure: Connectors and queues are designed to slow ingestion under load rather than drop data.
- Idempotency: Stable keys prevent duplication across retries and replays.

## Architecture at a glance
- Gateway: public API surface, request routing, auth proxying, and rate limiting.
- Orchestrator: workflow scheduling, triggers, and coordination of multi-step jobs.
- Connector Hub: managed connectors for external sources; handles discovery and schema drift.
- Normalizer: applies profiles to map, cleanse, validate, and enrich inputs.
- Analytics: aggregation, time-series, forecasting, and report generation.
- Storage: time-series, relational, cache, and blob storage backends.
- Audit: append-only ledger and compliance verification.
- Auth: identity providers, RBAC, and policy evaluation.
- Observer: metrics, tracing, and structured logging aggregation.
- Web: UI for operations, analytics, reports, and configuration.

## Repository layout
- `.github/`: CI workflows.
- `configs/`: environment-specific configuration files.
- `contracts/`: canonical schemas, validators, and code generation.
- `profiles/`: base and domain profiles for mappings and rules.
- `services/`: service source trees for the platform.
- `pkg/`: shared Go packages (contracts, profiles, telemetry, errors).
- `sdk/`: client SDKs for Go, Python, and TypeScript.
- `scripts/`: local dev, deploy, and testing scripts.
- `tests/`: unit, integration, and load test suites.
- `infra/`: Docker, Kubernetes, Helm, and Terraform definitions.
- `docs/`: system documentation and examples.
- `tools/`: developer tooling (profilers, schema generators).

## Non-goals for v0
- Full production UI workflows for all services.
- Multi-region active-active deployments.
- Automated marketplace of third-party connectors.
- Turn-key ML model training and retraining automation.

## Milestones
- v0: repository scaffolding, contracts/profile foundations, and minimal ingestion path.
- v1: multi-tenant operation, auth/audit integration, and baseline observability.
- v2: advanced analytics/reporting, ML-assisted insights, and compliance automation.

## Local development

### Quickstart (local)
```powershell
cd C:\Chartly2.0
docker compose up -d
docker compose ps
docker compose logs -f --tail=100
docker compose down
```

### Prerequisites
- Windows 10/11 with PowerShell 5.1+ (or PowerShell 7+)
- Docker Desktop (WSL2 backend recommended)
- Go (latest stable)
- Optional: Node.js for `web/`, Python for contract tooling

### Start the stack (Docker Compose)
```powershell
cd C:\Chartly2.0
docker compose up -d
```

### Run a single service locally (example: gateway)
> The service tree is scaffolded; the command below is the expected pattern once the service is implemented.
```powershell
$env:CHARTLY_CONFIG = "C:\Chartly2.0\configs\local.yaml"
go run .\services\gateway\cmd\gateway
```

## Configuration
- `configs/*.yaml` are the authoritative per-environment configuration sources.
- `.env.example` documents environment variables used by Docker Compose and services.
- Expected precedence (highest to lowest): environment variables > service config file (`configs/*.yaml`) > defaults in code.
- Configuration merging and validation are intended to live in `pkg/config` and service startup logic.

## Contracts & versioning
- Schemas live under `contracts/v1/` and are versioned by directory.
- Schema evolution should be additive for minor revisions; breaking changes require a new major version directory (e.g., `v2`).
- Validation tooling and code generation are scaffolded in `contracts/validators` and `contracts/codegen`.

## Profiles
- Profiles live in `profiles/` and define domain behavior: mapping, cleansing, deduplication, enrichment, retention, and alerts.
- Base profiles in `profiles/core/base/` provide defaults; domain profiles override and extend them.
- Authoring flow (roadmap): update profile YAMLs → lint/validate → run normalizer against fixtures → promote to staging/production.

## Observability
- Logs: services are expected to emit structured logs (JSON) with request and trace identifiers.
- Metrics: Prometheus-compatible metrics are defined via the observer service.
- Tracing: OpenTelemetry-compatible spans should be propagated by gateway and downstream services.
- The observability implementation is scaffolded but not yet wired end-to-end.

## Security & compliance
- RBAC policies and permissions are defined under `services/auth/internal/rbac/`.
- Secrets should be provided via environment variables or a secret store; do not commit secrets.
- Audit immutability is intended to be enforced by append-only storage and hash chaining.
- Compliance modules (GDPR/HIPAA/SOX) are scaffolded for future enforcement.

## Testing strategy
- Unit tests: `tests/unit/` plus service/package-level tests.
- Integration tests: `tests/integration/` (expected to use Docker Compose).
- Load tests: `tests/load/` (YAML scenarios).

PowerShell (unit tests, once implemented):
```powershell
go test ./...
```

WSL/Linux (integration/load scripts, once implemented):
```bash
wsl ./scripts/testing/integration_test.sh
wsl ./scripts/testing/load_test.sh
```

## Deployment overview
- `infra/docker/`: local and dev Docker configurations.
- `infra/k8s/`: Kubernetes manifests for services and monitoring.
- `infra/helm/`: Helm chart for Chartly.
- `infra/terraform/`: cloud infrastructure modules (VPC, EKS, RDS, Redis, S3).
- CI/CD workflows are scaffolded under `.github/workflows/` for staging and production promotion.

## Contributing
- Keep changes small and focused; prefer incremental commits with descriptive messages.
- Go code should be formatted with `gofmt` and follow standard error-handling patterns.
- Frontend changes (when implemented) should follow the linting/formatting rules defined in `web/`.
- PRs should include a brief design note, testing evidence, and any schema/profile changes.

