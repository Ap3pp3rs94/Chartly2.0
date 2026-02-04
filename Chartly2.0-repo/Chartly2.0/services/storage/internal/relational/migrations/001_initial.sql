-- Chartly 2.0  Storage Relational (PostgreSQL)  Migration 001
--
-- Purpose:
--   Create the initial durable object store table for the storage service.
--
-- Notes:
--   - This schema is intentionally minimal and maps to internal/relational/postgres_store.go.
--   - Metadata headers are stored as canonical JSON TEXT (not jsonb) to keep migrations simple and
--     avoid coupling to advanced PostgreSQL features.
--   - The storage service computes and stores SHA256 (hex) and bytes deterministically.
--   - No triggers are created here; application may control updated_at for reproducibility.
--
-- Conventions:
--   - tenant_id + object_key is the primary key (multi-tenant safe scoping).
--   - created_at/updated_at are timestamptz with defaults for manual SQL usage; app can override.

BEGIN;

-- Optional namespace to keep relational storage objects organized.
CREATE SCHEMA IF NOT EXISTS storage;

CREATE TABLE IF NOT EXISTS storage.chartly_objects (
  tenant_id     TEXT        NOT NULL,
  object_key    TEXT        NOT NULL,
  content_type  TEXT        NOT NULL,
  sha256        TEXT        NOT NULL, -- hex (64 chars)
  bytes         BIGINT      NOT NULL,
  headers_json  TEXT        NOT NULL, -- canonical JSON text
  body          BYTEA       NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chartly_objects_pkey PRIMARY KEY (tenant_id, object_key),

  -- Defensive constraints:
  CONSTRAINT chartly_objects_tenant_nonempty CHECK (length(btrim(tenant_id)) > 0),
  CONSTRAINT chartly_objects_key_nonempty    CHECK (length(btrim(object_key)) > 0),
  CONSTRAINT chartly_objects_ct_nonempty     CHECK (length(btrim(content_type)) > 0),

  CONSTRAINT chartly_objects_bytes_nonneg    CHECK (bytes >= 0),
  CONSTRAINT chartly_objects_sha256_len      CHECK (char_length(sha256) = 64),

  -- Ensure bytes matches payload length (prevents inconsistent writes).
  CONSTRAINT chartly_objects_bytes_match_body CHECK (bytes = octet_length(body))
);

-- Operational index: per-tenant recent activity.
CREATE INDEX IF NOT EXISTS chartly_objects_tenant_updated_idx
  ON storage.chartly_objects (tenant_id, updated_at DESC);

-- Optional: fast per-tenant hash lookup (dedupe/integrity workflows).
CREATE INDEX IF NOT EXISTS chartly_objects_tenant_sha_idx
  ON storage.chartly_objects (tenant_id, sha256);

COMMIT;
