#!/usr/bin/env bash
set -euo pipefail

# Chartly contracts -> Go codegen (optional)
# This script is intentionally non-fatal if quicktype is not installed.
#
# Output: ./pkg/canonical_gen/*.go (one file per schema)
#
# Safety: does not overwrite existing generated output unless FORCE=1 is set.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCHEMA_DIR="${ROOT_DIR}/contracts/v1/canonical"
OUT_DIR="${ROOT_DIR}/pkg/canonical_gen"
PKG_NAME="canonical_gen"

if ! command -v quicktype >/dev/null 2>&1; then
  echo "quicktype is not installed; skipping Go codegen."
  echo "Install (one option): npm i -g quicktype"
  echo "Then re-run: contracts/codegen/generate_go.sh"
  exit 0
fi

# Enforce clean output directory unless explicitly overridden
if [ -d "${OUT_DIR}" ] && [ "${FORCE:-}" != "1" ]; then
  if ls "${OUT_DIR}"/*.go >/dev/null 2>&1; then
    echo "${OUT_DIR} already contains generated files."
    echo "Refusing to overwrite. Re-run with FORCE=1 to regenerate."
    exit 0
  fi
fi

mkdir -p "${OUT_DIR}"

echo "Generating Go types from schemas in ${SCHEMA_DIR}"
for schema in "${SCHEMA_DIR}"/*.schema.json; do
  base="$(basename "${schema}")"
  name="${base%.schema.json}"          # entity, event, etc.
  out="${OUT_DIR}/${name}.go"

  echo " - ${base} -> ${out}"

  # quicktype json-schema -> go
  # Note: quicktype uses a "top-level" name; we set it to TitleCase of schema name.
  quicktype \
    --lang go \
    --package "${PKG_NAME}" \
    --top-level "$(echo "${name}" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')" \
    --src "${schema}" \
    --out "${out}"
done

echo "Done. Generated files in ${OUT_DIR}"
