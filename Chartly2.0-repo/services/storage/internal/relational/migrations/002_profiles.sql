-- Chartly 2.0  Storage Relational (PostgreSQL)  Migration 002
--
-- Purpose:
--   Add a tenant-scoped "profiles" table for storing small profile documents as durable objects.
--
-- What is a "profile"?
--   A profile is a DATA-ONLY document (usually JSON) that defines configuration presets or catalogs:
--     - connector profiles
--     - chart template catalogs
--     - pipeline/workflow configs
--     - user/system presets
--   Services interpret these profiles; this table stores them safely and durably.
--
-- Design:
--   - Multi-tenant safe: tenant_id is always required; primary key is (tenant_id, profile_key).
--   - Deterministic integrity: sha256 + bytes stored and validated; bytes must match body length.
--   - Headers/metadata are stored as canonical JSON TEXT (not jsonb) to keep dependencies minimal.
--   - No triggers are created; application may manage updated_at for reproducibility.

BEGIN;

CREATE SCHEMA IF NOT EXISTS storage;

CREATE TABLE IF NOT EXISTS storage.chartly_profiles (
  tenant_id     TEXT        NOT NULL,
  profile_key   TEXT        NOT NULL, -- stable identifier (e.g. "connectors/default", "charts/templates")
  profile_type  TEXT        NOT NULL, -- e.g. "connector_profile", "chart_templates", "pipeline_config"
  content_type  TEXT        NOT NULL DEFAULT 'application/json',
  schema        TEXT        NULL,     -- optional schema name (e.g. "chartly.profile.v1")
  version       TEXT        NULL,     -- optional version tag (e.g. "1.0.0")

  sha256        TEXT        NOT NULL, -- hex (64 chars)
  bytes         BIGINT      NOT NULL,
  meta_json     TEXT        NOT NULL, -- canonical JSON text for small metadata (data-only)
  body          BYTEA       NOT NULL, -- raw UTF-8 JSON bytes (or other content-type if needed)

  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chartly_profiles_pkey PRIMARY KEY (tenant_id, profile_key),

  -- Defensive constraints:
  CONSTRAINT chartly_profiles_tenant_nonempty CHECK (length(btrim(tenant_id)) > 0),
  CONSTRAINT chartly_profiles_key_nonempty    CHECK (length(btrim(profile_key)) > 0),
  CONSTRAINT chartly_profiles_type_nonempty   CHECK (length(btrim(profile_type)) > 0),
  CONSTRAINT chartly_profiles_ct_nonempty     CHECK (length(btrim(content_type)) > 0),

  CONSTRAINT chartly_profiles_bytes_nonneg    CHECK (bytes >= 0),
  CONSTRAINT chartly_profiles_sha256_len      CHECK (char_length(sha256) = 64),

  -- Ensure bytes matches payload length (prevents inconsistent writes).
  CONSTRAINT chartly_profiles_bytes_match_body CHECK (bytes = octet_length(body))
);

-- Operational indexes:
-- 1) Quickly list recent updates per tenant.
CREATE INDEX IF NOT EXISTS chartly_profiles_tenant_updated_idx
  ON storage.chartly_profiles (tenant_id, updated_at DESC);

-- 2) Quickly fetch by profile_type within tenant.
CREATE INDEX IF NOT EXISTS chartly_profiles_tenant_type_key_idx
  ON storage.chartly_profiles (tenant_id, profile_type, profile_key);

-- 3) Optional integrity/dedupe workflows by hash within tenant.
CREATE INDEX IF NOT EXISTS chartly_profiles_tenant_sha_idx
  ON storage.chartly_profiles (tenant_id, sha256);

COMMIT;
