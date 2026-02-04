# Chartly 2.0  Security

## Contract status & trust model

This document defines the **security contract** for Chartly: what is protected, how trust is established, and which rules are non-negotiable across the platform.

### Legend
-  **Implemented**  verified in code and/or conformance tests
- 🛠 **Planned**  desired contract, may not exist yet
- 🧪 **Experimental**  available but may change without full deprecation guarantees

**Rule:** Any security control not explicitly marked  is 🛠.

### Promotion criteria (🛠  )
A security control becomes  only when:
- it is enforced by default (not advisory),
- bypass requires explicit configuration,
- and at least one negative test proves enforcement.

---

## Security philosophy

Chartly is designed under a **zero-trust, least-privilege** model.

- Trust is **never implicit**
- Access is **explicitly granted**
- Secrets are **never embedded**
- Security controls are **deterministic and auditable**

Security MUST NOT:
- depend on developer discipline alone
- rely on obscurity
- be environment-specific without documentation

---

## Threat model (baseline)

Chartly assumes the following threat classes exist:

- Compromised credentials
- Malicious or buggy connectors
- Misconfigured deployments
- Insider misuse
- External dependency failures

The platform is designed to **limit blast radius**, **preserve auditability**, and **fail safely** under these conditions.

---

## Identity & authentication

### Identity sources (provider-neutral)
Chartly integrates with external identity systems that issue verifiable credentials.

- Tokens are treated as **opaque**
- Validation happens at the Gateway
- Internal services trust identity only via validated context

### Authentication rules
- All external requests MUST be authenticated (except health probes)
- Tokens MUST be time-bound
- Token validation failures MUST fail closed

### Starter auth controls (implementation)
The control plane supports simple, provider-neutral auth gates:
- `AUTH_REQUIRED=true` enforces `X-Principal` on all non-health endpoints
- `AUTH_TENANT_REQUIRED=true` enforces `X-Tenant-ID` alongside `X-Principal`
- Gateway supports API key files and JWT HS256 secret file (see `docs/DEPLOYMENT.md`)

These are **starter controls** and should be replaced by a real IdP + RBAC in production.

---

## Authorization & RBAC

### Authorization model
Chartly uses **role-based access control (RBAC)** with explicit permissions.

- Permissions are additive
- Default is deny-all
- Authorization is enforced at:
  - Gateway
  - Service boundaries (defense-in-depth)

### Scope principles
- Scopes grant capability, not identity
- Broad scopes are forbidden in production
- Service-to-service scopes are minimized

---

## Network security

### Network segmentation
- Control plane and data plane MUST be isolated
- East-west traffic SHOULD be restricted
- No implicit ingress or egress

### Egress control (hard rule)
- All outbound traffic MUST be allow-listed
- DNS allow-lists preferred
- Private CIDRs blocked by default unless explicitly approved

---

## Secrets management

### Hard rules
- Secrets MUST NOT appear in:
  - source code
  - profiles
  - manifests
  - logs
- Secrets MUST be referenced, never embedded

### Secret references
- `secretRef` objects contain only:
  - secret name
  - key identifier
- Rotation MUST be supported without redeploying code

---

## Data protection

### Data in transit
- TLS required for all external traffic
- Internal encryption strongly recommended
- Certificate validation MUST be enabled

### Data at rest
- Encryption at rest REQUIRED where supported
- Access to storage mediated by service identity
- No shared credentials across services

---

## Runtime security

### Container hardening (mandatory)
- Run as non-root
- Read-only root filesystem
- Drop all Linux capabilities
- Disable privilege escalation
- Explicit writable mounts only

### Execution boundaries
- No dynamic code execution
- No shell access in production
- Debug endpoints disabled by default

---

## API & input security

### Input validation
- Strict schema validation
- Unknown fields rejected
- Size limits enforced

### Abuse protection
- Rate limiting at Gateway
- Idempotency enforcement
- Replay protection for webhooks

---

## Connector-specific security

Connectors represent the **highest-risk surface**.

### Mandatory controls
- Profile-driven behavior only
- SSRF protection enforced
- Payload size limits
- Timeout and retry bounds
- Strict content-type validation

### Inbound webhook controls
- Signature verification
- Timestamp skew checks
- Idempotency keys required

---

## Audit & accountability

### Audit events MUST record
- Who performed an action
- What changed
- When it occurred
- Where it was executed
- Which identity was used

### Audit guarantees
- Audit logs are append-only
- Audit data is immutable
- Audit retention follows policy

---

## Incident response principles

Chartly is designed to support rapid containment.

### Containment levers
- Disable ingress at Gateway
- Revoke or rotate credentials
- Scale down data-plane services
- Preserve audit evidence

### Post-incident
- No data deletion without review
- Root cause documented
- Preventive controls added

---

## Security invariants (non-negotiable)

- No secrets in code or config
- No implicit trust between services
- No unauthenticated data mutation
- No unbounded external access
- All actions are auditable

Violating any invariant invalidates the deployment.

---

## Operator checklist

Before production use:
- [ ] Authentication enforced
- [ ] RBAC scopes reviewed
- [ ] Network policies applied
- [ ] Secrets externalized
- [ ] Containers hardened
- [ ] Rate limits enabled
- [ ] Audit logging verified
- [ ] Incident runbook prepared

---

## Next steps (🛠)

- Add security conformance tests:
  - unauthorized access attempts
  - SSRF attempts
  - secret leakage checks
- Add automated policy enforcement
- Promote core controls to 
