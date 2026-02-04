#!/usr/bin/env bash
set -euo pipefail

# Chartly contracts -> TypeScript codegen (optional)
# This script is intentionally non-fatal if quicktype is not installed.
#
# Output: ./web/src/contracts_gen/*.ts (one file per schema)

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${ROOT_DIR}/web/src/contracts_gen"

SCHEMA_DIRS=(
  "${ROOT_DIR}/contracts/v1/canonical"
  "${ROOT_DIR}/contracts/v1/telemetry"
  "${ROOT_DIR}/contracts/v1/reports"
)

if ! command -v quicktype >/dev/null 2>&1; then
  echo "quicktype is not installed; skipping TypeScript codegen."
  echo "Install (one option): npm i -g quicktype"
  echo "Then re-run: contracts/codegen/generate_ts.sh"
  exit 0
fi

# Enforce clean output directory unless explicitly overridden
if [ -d "${OUT_DIR}" ] && [ "${FORCE:-}" != "1" ]; then
  if ls "${OUT_DIR}"/*.ts >/dev/null 2>&1; then
    echo "${OUT_DIR} already contains generated files."
    echo "Refusing to overwrite. Re-run with FORCE=1 to regenerate."
    exit 0
  fi
fi

mkdir -p "${OUT_DIR}"

to_pascal() {
  local s="$1"
  # snake/kebab to PascalCase
  echo "$s" | awk -F'[-_]' '{for(i=1;i<=NF;i++){ $i=toupper(substr($i,1,1)) substr($i,2)}; printf "%s", $0}'
}

echo "Generating TypeScript types into ${OUT_DIR}"

for dir in "${SCHEMA_DIRS[@]}"; do
  if [ ! -d "$dir" ]; then
    continue
  fi
  for schema in "$dir"/*.schema.json; do
    [ -e "$schema" ] || continue
    base="$(basename "$schema")"
    name="${base%.schema.json}"
    out="${OUT_DIR}/${name}.ts"
    top="$(to_pascal "$name")"
    echo " - ${base} -> ${out} (${top})"
    quicktype \
      --lang typescript \
      --just-types \
      --top-level "${top}" \
      --src "${schema}" \
      --out "${out}"
  done
done

echo "Done. Generated files in ${OUT_DIR}"
