# /infra/docker  Authority Contract (Chartly 2.0)

This folder is the **single source of truth** for Docker **orchestration and deployment templates**.

## Decision

**infra/docker/** contains:
- Compose templates for environments (e.g. `compose.dev.yml`, `compose.staging.yml`, `compose.prod.yml`)
- Non-secret env templates (e.g. `.env.example`)
- Documentation and notes about container orchestration
- Deployment-facing Docker assets (if/when needed), but **not** per-service build logic

**infra/docker/** does NOT contain:
- Service Dockerfiles
- Build scripts / helper scripts
- Secrets or real `.env` values
- Random one-off compose files without naming convention

## Source-of-truth rules

### Service images
- **Service Dockerfiles live with the service**:
  - `services/<service>/Dockerfile`
- A service should be buildable in isolation from its own directory.

### Dev compose (local)
- **Temporary allowance**: repo-root `docker-compose.yml` may exist for early dev.
- If it exists, it is considered **legacy** and must not diverge from the canonical template.

### Staging/Prod compose
- Environment templates live here:
  - `infra/docker/compose.staging.yml`
  - `infra/docker/compose.prod.yml`

### Scripts
- Operational scripts belong under:
  - `scripts/` (developer workflows)
- Scripts should **reference** infra/docker templates rather than embedding orchestration logic.

## Naming convention

- `compose.<env>.yml` where `<env>` is `dev`, `staging`, `prod`
- `.env.<env>.example` for non-secret templates

## Migration plan (when ready)

1) Create `infra/docker/compose.dev.yml` as the canonical dev template.  
2) Add a lightweight script (later) that runs:
   - `docker compose -f infra/docker/compose.dev.yml up -d`
3) Replace repo-root compose with either:
   - a thin pointer README, or
   - remove it entirely once all tooling references infra/docker.

Until step (3), **do not edit compose files in multiple places**.
All changes must be made in `infra/docker/` first, then mirrored once if legacy is still present.
