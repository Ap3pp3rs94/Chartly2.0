# Changelog
All notable changes to this project are documented in this file.

This project follows **Semantic Versioning**:
- MAJOR: incompatible contract or architecture changes
- MINOR: backward-compatible functionality
- PATCH: backward-compatible fixes or clarifications

---

## [0.0.1] - 2026-01-21

### Added
- Initial Chartly 2.0 repository scaffold.
- Canonical project structure for services, contracts, profiles, infrastructure, SDKs, and tooling.
- Contracts-first foundation with versioned schema directories.
- Profiles-over-code directory structure for mappings, cleansing, deduplication, enrichment, retention, and alerts.
- MANIFEST.yaml defining the project law, service graph, data planes, and invariants.
- ARCHITECTURE.md describing service boundaries, data flows, and scaling model.
- Local development support via Docker Compose (infrastructure-level only).
- CI/CD workflow placeholders for linting, testing, and deployments.
- Production-ready repository hygiene files (.gitignore, .editorconfig, LICENSE).

### Changed
- N/A (initial release).

### Fixed
- N/A (initial release).

### Deprecated
- N/A (initial release).

### Removed
- N/A (initial release).

### Security
- Established baseline security rules:
  - No secrets committed to the repository.
  - Auditability and quarantine defined as mandatory invariants.
  - RBAC and compliance hooks scoped for future implementation.

---

## Versioning notes

- Pre-1.0 releases (`0.x.y`) may introduce structural changes as the architecture is finalized.
- Contract breaking changes will always be accompanied by a new major contract directory (e.g., `contracts/v2/`).
- The changelog must be updated with every merged change that affects behavior, contracts, profiles, or operations.
