# Security Policy  Chartly 2.0

## 1) Purpose and scope

This document defines the **security expectations and guarantees** for the Chartly 2.0 codebase and runtime.
It applies to all services, tooling, infrastructure definitions, and contributors.

This is a **policy document**, not a checklist. Implementation details may evolve, but the rules here are
non-negotiable unless explicitly amended via versioned change.

---

## 2) Security principles

Chartly 2.0 is designed around the following core security principles:

### Contracts-first enforcement
- All externally facing payloads and all canonical outputs are validated against versioned contracts.
- Schema validation failures are treated as security-relevant events and routed to quarantine.

### Least privilege
- Services operate with the minimum permissions required for their role.
- Cross-service access is explicitly defined; implicit trust is not allowed.

### Defense in depth
- Multiple layers of validation exist (gateway, normalizer, storage).
- A single failure must not compromise raw truth, canonical integrity, or auditability.

### Assume breach
- Systems are designed to contain damage (quarantine, idempotency, append-only audit).
- Detection and traceability are prioritized over silent failure.

---

## 3) Secrets management

### Repository rules
- **Secrets MUST NOT be committed** to the repository.
- The following locations must never contain secrets:
  - `configs/*.yaml`
  - `profiles/**/*.yaml`
  - `contracts/**/*.json`
  - Any source-controlled code file

### Runtime rules
- Secrets must be provided via:
  - Environment variables, or
  - An external secret manager (cloud-native or Vault-style)
- `.env.example` documents variable names only and must never contain real credentials.

### Rotation
- Secrets must be rotatable without code changes.
- Long-lived credentials are discouraged; prefer short-lived tokens where possible.

---

## 4) Authentication and authorization

### Authentication
- Authentication is enforced at the **gateway**.
- Supported mechanisms (incremental):
  - JWT (v0 baseline)
  - OAuth2 / SAML (roadmap)
- Internal services must not trust unauthenticated external requests.

### Authorization (RBAC)
- Authorization decisions are policy-driven and centralized in `services/auth`.
- Roles and permissions are defined declaratively.
- Gateway enforces RBAC before routing to internal services.

### Service-to-service access
- Internal calls must include service identity and correlation metadata.
- No service should have blanket access to all others.

---

## 5) Data protection by plane

### Raw data plane
- Raw payloads are immutable once written.
- Stored in blob storage with:
  - content hash (sha256)
  - capture timestamp
  - source identifier
- Raw data may contain sensitive fields and must be access-restricted.

### Canonical data plane
- Only validated, policy-compliant records are written.
- Canonical records MUST reference raw data via `raw_ref`.
- Direct writes to canonical storage bypassing the normalizer are forbidden.

### Audit data plane
- Audit records are append-only.
- Hash chaining is used to detect tampering.
- Audit data must be readable for verification but not mutable.

---

## 6) PII / PHI handling

### Declaration
- Profiles MUST declare whether they handle PII or PHI.
- Data classification is explicit, not inferred.

### Enforcement
- Normalizer enforces profile-declared constraints.
- Violations result in quarantine with reason codes.
- Silent redaction without audit is forbidden.

### Compliance hooks
- GDPR, HIPAA, and SOX requirements are implemented as policy layers, not ad-hoc logic.
- Compliance-specific behavior must be auditable and reversible where legally required.

---

## 7) Network and transport security

- All external communication must use TLS.
- Internal service communication must use secure transport where supported by the environment.
- Gateway is the only service exposed to the public network by default.
- Network policies must restrict lateral movement between services.

---

## 8) Dependency and supply-chain security

- Dependencies must be pinned (Go modules, npm lockfiles, Python requirements).
- CI pipelines should include:
  - Dependency vulnerability scanning
  - License checks
- Unmaintained or untrusted dependencies should be avoided.

---

## 9) Logging, monitoring, and incident response

### Logging
- Logs are structured and include:
  - timestamp
  - service name
  - environment
  - request_id / job_id
  - tenant_id (when applicable)
- Sensitive data must not be logged.

### Monitoring
- Metrics are emitted for error rates, queue depth, and abnormal behavior.
- Alerts should trigger on sustained failures, not single events.

### Incident response
- Security incidents must be:
  1. Detected (logs/metrics)
  2. Contained (disable sources, tighten rate limits, quarantine data)
  3. Investigated (audit records, raw payloads)
  4. Documented and remediated

---

## 10) Responsible disclosure

If you discover a security vulnerability:
- Do **not** open a public issue.
- Contact the maintainers privately with:
  - A clear description of the issue
  - Steps to reproduce
  - Potential impact

Responsible disclosures will be acknowledged and addressed promptly.

---

## Policy changes

Changes to this document require:
- Review by maintainers
- A changelog entry
- Versioned update if guarantees are weakened or expanded
