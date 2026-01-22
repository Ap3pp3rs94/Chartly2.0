# Chartly 2.0  Profiles

Profiles define **domain behavior** in Chartly 2.0 using configuration rather than hard-coded logic.
Chartly is **profiles-over-code**: services execute profile rules to map raw payloads into canonical records, enforce policy,
apply cleansing and deduplication, and control retention and alerting.

Profiles are designed to make onboarding new sources and domains repeatable, auditable, and safe.

---

## What a profile controls

A profile can define:

- **Mapping**: raw fields  canonical entities/events/metrics
- **Cleansing**: type fixes, normalization, trimming, canonical formatting rules
- **Deduplication**: idempotency keys, record-level dedupe strategies
- **Enrichment**: derived fields, lookup/enrichment steps, controlled joins
- **Validation rules**: domain constraints beyond schema validation
- **Retention**: lifecycle policies for raw/canonical/audit data
- **Alerts**: thresholds and triggers for anomaly detection or operational health

---

## Directory layout

- `profiles/core/base/`  
  The baseline profile bundle used by the normalizer as default behavior.

- `profiles/core/connectors/`  
  Connector-oriented profile templates (http_rest, graphql, database, webhook, etc.).

- `profiles/domains/<domain>/`  
  Domain-specific overlays and rulesets (finance/healthcare/ecommerce, etc.).

- `profiles/templates/`  
  Templates for creating new profiles and mappings.

- `profiles/tests/`  
  Profile lint configuration and fixtures used by profile validation tooling.

---

## Required files in a profile bundle

Each profile bundle should include the following files:

- `profile.yaml`  
  Metadata: profile name, version, domain, ownership, PII/PHI classification, and compatibility.

- `mappings.yaml`  
  Mapping rules for converting raw payloads into canonical objects (Metric/Event/Entity, etc.).

- `rules.yaml`  
  Domain validation rules beyond contract schema enforcement (ranges, allowed values, required dimensions).

- `cleansing.yaml`  
  Transformations applied before mapping/validation (string trimming, timestamp normalization, unit conversion).

- `deduplication.yaml`  
  Idempotency and dedupe strategy:
  - keys used to dedupe repeats
  - time windows (if any)
  - collision handling

- `enrichment.yaml`  
  Enrichment steps (optional) such as:
  - derived fields
  - lookup tables
  - safe external enrichers (policy-controlled)

- `retention.yaml`  
  Lifecycle controls:
  - raw retention (days)
  - canonical retention (days)
  - audit retention (days)
  - compaction/purge policy (as implemented)

- `alerts.yaml`  
  Alert definitions:
  - thresholds
  - evaluation windows
  - routing/notification policy (as implemented)

---

## How profiles are used in the system

The normalizer service is responsible for applying profiles (intended architecture):

1) Fetch raw payload reference (`raw_ref`) written by connector-hub.
2) Load applicable profile bundle(s) for the source/domain.
3) Apply `cleansing.yaml` transformations.
4) Apply `mappings.yaml` to produce candidate canonical records.
5) Validate against:
   - contracts (JSON Schemas), and
   - profile `rules.yaml` domain constraints
6) Apply `deduplication.yaml` idempotency checks.
7) Apply `enrichment.yaml` (if enabled).
8) Write valid canonical records and quarantine invalid records with reason codes.

Profiles are versioned. Every canonical record and quarantine entry should include the profile version used.

---

## Authoring workflow (recommended)

1) Start from a template:
   - `profiles/templates/new_domain_template.yaml`
   - `profiles/templates/new_mapping_template.yaml`
2) Implement `profile.yaml` metadata and declare PII/PHI handling.
3) Define mappings and rules.
4) Validate locally using profile tooling (planned in `tools/profiler/`).
5) Run a controlled ingest against fixtures or a small source set.
6) Promote to staging via PR review.
7) Promote to production via tagged release.

---

## PII / PHI guidance

- Profiles must declare whether they handle PII/PHI.
- Do not write PII/PHI into logs.
- Ensure quarantined items include reason codes but do not leak sensitive payload content.
- Use policy gates for any enrichment that may expand sensitive content.

---

## Deduplication and idempotency guidance

At scale, duplicates are normal (retries, replays, upstream duplicates).

Profiles should define:
- stable idempotency keys (tenant_id + source_id + raw hash + profile version is a strong baseline)
- dedupe windows (only if required)
- collision behavior (keep-first vs keep-last, quarantine on conflict, etc.)

---

## Retention policy guidance

Retention should be explicit per domain and environment. Typical patterns:
- raw payloads: shorter retention (but long enough for debugging/replay)
- canonical records: longer retention (analytics/history)
- audit: longest retention (compliance)

Retention should be implemented as lifecycle jobs controlled by orchestrator (planned).

---

## What profiles are not

- Profiles are not secrets storage.
- Profiles are not a substitute for contracts; they complement contracts with domain rules.
- Profiles should not embed arbitrary code; keep them declarative and reviewable.
