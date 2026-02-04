# Chartly 2.0  Contracts-First Data Platform

**Production-ready contracts-first data platform. 9 microservices. Deploy in 60 seconds.**

Chartly 2.0 is a deterministic, contracts-first data platform built to ingest, normalize, analyze, and report across domains without silent drift.

- `services/`  9 microservices (Go)
- `infra/`  Kubernetes, Helm, Terraform
- `contracts/`  JSON schemas + validators
- `profiles/`  YAML configs (behavior contracts)
- `sdk/`  Go, Python, TypeScript
- MIT licensed

**Quick Links**
- `docs/DEPLOYMENT.md`
- `docs/PROFILES.md`
- `docs/API.md`
- `docs/TROUBLESHOOTING.md`

---

**60-Second Quickstart (Docker Compose)**

From repo root:

```powershell
docker compose -f docker-compose.control.yml build
docker compose -f docker-compose.control.yml up -d
```

Open:
- `http://localhost:8090/`

---

**Verification Commands**

```powershell
Invoke-RestMethod http://localhost:8090/health
Invoke-RestMethod http://localhost:8090/api/status
Invoke-RestMethod http://localhost:8090/api/profiles
Invoke-RestMethod http://localhost:8090/api/drones
Invoke-RestMethod http://localhost:8090/api/results/summary
```

Logs:

```powershell
docker compose -f docker-compose.control.yml logs -f --tail 200
```

---

**Architecture (ASCII)**

```
                    +-----------------------+
                    |      Web UI / API     |
                    |   gateway (8080)      |
                    +-----------+-----------+
                                |
        +-----------------------+-----------------------+
        |                       |                       |
+-------v-------+       +-------v-------+       +-------v-------+
|   registry    |       |  coordinator  |       |  aggregator   |
| profiles      |       | drones/queue  |       | results/runs  |
+-------+-------+       +-------+-------+       +-------+-------+
        |                       |                       |
        +-----------------------+-----------------------+
                                |
                        +-------v-------+
                        |    reporter   |
                        | joins/reports |
                        +---------------+
```

---

**Service Catalog**

| Service | Purpose | Health Endpoint |
|---|---|---|
| gateway | Single public entrypoint + routing + UI | `/health` |
| registry | Profile storage and serving | `/health` |
| coordinator | Drone registry and work queue | `/health` |
| aggregator | Results + runs storage | `/health` |
| reporter | Join/correlation reports | `/health` |
| analytics | Analytics + reporting | `/health` |
| storage | Dataset + artifact storage | `/health` |
| auth | Identity + RBAC | `/health` |
| audit | Append-only audit ledger | `/health` |

---

**Contracts & Profiles Doctrine**

Contracts:
- JSON schemas live under `contracts/`
- Deterministic validation and explicit versioning
- Drift is treated as a failure mode

Profiles:
- YAML configs live under `profiles/`
- Profiles are configuration contracts, not scripts
- No secrets in profiles (use env or secret references)
- Profiles should be versioned and reviewable

Determinism:
- Same inputs + same profile versions + same window bounds  same output
- Tools emit stable hashes to prevent silent edits

---

**Tools Overview**

- `tools/schema-gen/`  deterministic contract artifact generation + drift verification
- `tools/migration-tool/`  deterministic planning + safety-gated apply/verify
- `tools/profiler/`  read-only deterministic profiler entrypoint
- `tools/connector-tester/`  connector validation harness with plan/run/validate semantics

---

**SDKs**

- `sdk/go/`
- `sdk/python/`
- `sdk/typescript/`

Each SDK directory includes examples and client scaffolding.

---

**Troubleshooting (Quick Hits)**

- Gateway health fails:
  - `docker compose -f docker-compose.control.yml logs --tail 200`
- UI loads but lists are empty:
  - `Invoke-RestMethod http://localhost:8090/api/status`
- No results:
  - Ensure drones/executors are running and posting results
  - Check aggregator logs

---

**Roadmap (Short, Realistic)**

- Promote planned endpoints to implemented via conformance tests
- Expand overlap reporting primitives (joins + correlation)
- Harden RBAC enforcement and audit completeness
- Enforce drift detection automatically in CI

---

**Contributing**

- Keep behavior deterministic
- No secrets in repo
- Prefer contract-first updates: schemas + profiles + tools + tests

**License**

MIT  see `LICENSE`.
