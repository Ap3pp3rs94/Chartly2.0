# Security  (Chartly 2.0)

Chartly defaults are **secure-by-default** and provider-neutral.

## Runtime Baseline

- `runAsNonRoot: true`
- `seccompProfile: RuntimeDefault`
- `allowPrivilegeEscalation: false`
- `capabilities.drop: ["ALL"]`
- `readOnlyRootFilesystem: true`
- explicit writable mounts for `/tmp` and data paths

## Secrets

No secrets are stored in this repo. Supply secrets using:

- External Secrets Operator
- Sealed Secrets
- CI/CD secret injection

## Headers

Clients should propagate:

- `x-request-id`
- `traceparent` (W3C)
- `x-chartly-tenant` (when applicable)

## Audit

The Audit service provides immutable logging for critical actions.
