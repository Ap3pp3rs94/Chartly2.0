-- Chartly 2.0  Storage Relational (PostgreSQL)  Migration 003
--
-- Purpose:
--   Add an append-only audit log for storage service activity.
--
-- Notes:
--   - This table is DATA ONLY. It records what happened; enforcement/alerting is handled by services.
--   - event_id and event_ts are caller-provided for determinism and traceability.
--   - detail_json is stored as TEXT (canonical JSON) to keep dependencies minimal and portable.
--   - No triggers are created in this migration.

BEGIN;

CREATE SCHEMA IF NOT EXISTS storage;

CREATE TABLE IF NOT EXISTS storage.chartly_audit (
  tenant_id    TEXT         NOT NULL,
  event_id     TEXT         NOT NULL, -- caller-provided unique id per tenant
  event_ts     TIMESTAMPTZ  NOT NULL, -- caller-provided timestamp
  action       TEXT         NOT NULL, -- e.g. put|get|head|delete|meta|stats
  object_key   TEXT         NULL,     -- object scoped events
  request_id   TEXT         NULL,     -- upstream X-Request-Id
  actor_id     TEXT         NULL,     -- user/service identity if available
  source       TEXT         NULL,     -- e.g. "storage-http"
  outcome      TEXT         NOT NULL, -- ok|not_found|error
  detail_json  TEXT         NOT NULL, -- canonical JSON, may be "{}"

  created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

  CONSTRAINT chartly_audit_pkey PRIMARY KEY (tenant_id, event_id),

  -- Defensive constraints:
  CONSTRAINT chartly_audit_tenant_nonempty   CHECK (length(btrim(tenant_id)) > 0),
  CONSTRAINT chartly_audit_eventid_nonempty  CHECK (length(btrim(event_id)) > 0),
  CONSTRAINT chartly_audit_action_nonempty   CHECK (length(btrim(action)) > 0),
  CONSTRAINT chartly_audit_outcome_nonempty  CHECK (length(btrim(outcome)) > 0),
  CONSTRAINT chartly_audit_detail_nonempty   CHECK (length(btrim(detail_json)) > 0)
);

-- Operational indexes:
-- 1) Recent audit events per tenant.
CREATE INDEX IF NOT EXISTS chartly_audit_tenant_ts_idx
  ON storage.chartly_audit (tenant_id, event_ts DESC);

-- 2) Recent events per object within tenant.
CREATE INDEX IF NOT EXISTS chartly_audit_tenant_object_ts_idx
  ON storage.chartly_audit (tenant_id, object_key, event_ts DESC);

-- 3) Recent events per action within tenant.
CREATE INDEX IF NOT EXISTS chartly_audit_tenant_action_ts_idx
  ON storage.chartly_audit (tenant_id, action, event_ts DESC);

COMMIT;
