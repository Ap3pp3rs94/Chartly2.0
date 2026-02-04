#!/usr/bin/env python3
"""
Chartly Contracts Validator (dependency-free)

Validates:
- JSON parsing of all schemas under contracts/v1/**
- minimal structural requirements for Draft 2020-12 schemas
- basic $ref sanity (file refs exist; fragment refs allowed)
- fixtures under contracts/validators/fixtures (if any):
    - must include "__schema": "<relative schema path>"
    - required-field presence check based on schema["required"]

This tool is intentionally dependency-free to run in CI without pip installs.
"""
from __future__ import annotations

import json
import os
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Tuple

REPO_ROOT = Path(__file__).resolve().parents[2]  # .../contracts/validators/validate.py -> repo root
SCHEMA_DIRS = [
    REPO_ROOT / "contracts" / "v1" / "canonical",
    REPO_ROOT / "contracts" / "v1" / "telemetry",
    REPO_ROOT / "contracts" / "v1" / "reports",
]
FIXTURES_DIR = REPO_ROOT / "contracts" / "validators" / "fixtures"


@dataclass
class Error:
    path: str
    message: str


def eprint(msg: str) -> None:
    print(msg, file=sys.stderr)


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception as ex:
        raise ValueError(f"Invalid JSON: {ex}") from ex


def walk_refs(obj: Any, found: List[str]) -> None:
    if isinstance(obj, dict):
        for k, v in obj.items():
            if k == "$ref" and isinstance(v, str):
                found.append(v)
            else:
                walk_refs(v, found)
    elif isinstance(obj, list):
        for item in obj:
            walk_refs(item, found)


def validate_schema(schema_path: Path) -> List[Error]:
    errs: List[Error] = []
    rel = str(schema_path.relative_to(REPO_ROOT)).replace("\\", "/")

    try:
        schema = load_json(schema_path)
    except Exception as ex:
        return [Error(rel, str(ex))]

    if not isinstance(schema, dict):
        errs.append(Error(rel, "Schema root must be a JSON object."))
        return errs

    # Required keys
    for k in ("$schema", "$id", "title", "type"):
        if k not in schema:
            errs.append(Error(rel, f"Missing required key: {k}"))

    # Draft check
    sch = schema.get("$schema")
    if isinstance(sch, str):
        if "2020-12" not in sch or "json-schema" not in sch:
            errs.append(Error(rel, f"Unexpected $schema value (expected Draft 2020-12): {sch!r}"))
    elif sch is not None:
        errs.append(Error(rel, "$schema must be a string."))

    # $id check
    sid = schema.get("$id")
    if isinstance(sid, str):
        if not sid.strip():
            errs.append(Error(rel, "$id must be a non-empty string."))
    elif sid is not None:
        errs.append(Error(rel, "$id must be a string."))

    # title check
    title = schema.get("title")
    if title is not None and (not isinstance(title, str) or not title.strip()):
        errs.append(Error(rel, "title must be a non-empty string."))

    # type check
    stype = schema.get("type")
    if stype is not None and not isinstance(stype, (str, list)):
        errs.append(Error(rel, "type must be a string or array of strings."))

    # $ref sanity
    refs: List[str] = []
    walk_refs(schema, refs)
    for r in refs:
        if r.startswith("#"):
            # Fragment refs are allowed; we do not resolve them here.
            continue
        # If it's a URL, allow (remote resolution not performed)
        if "://" in r:
            continue
        # Otherwise treat as relative file ref (common pattern)
        ref_path = (schema_path.parent / r).resolve()
        try:
            ref_path.relative_to(REPO_ROOT)
        except Exception:
            errs.append(Error(rel, f"$ref points outside repo root: {r!r}"))
            continue
        if not ref_path.exists():
            errs.append(Error(rel, f"$ref file does not exist: {r!r} (resolved {ref_path})"))

    return errs


def validate_fixture(fixture_path: Path, schemas_cache: Dict[str, Dict[str, Any]]) -> List[Error]:
    errs: List[Error] = []
    rel = str(fixture_path.relative_to(REPO_ROOT)).replace("\\", "/")

    try:
        fx = load_json(fixture_path)
    except Exception as ex:
        return [Error(rel, str(ex))]

    if not isinstance(fx, dict):
        errs.append(Error(rel, "Fixture root must be a JSON object."))
        return errs

    schema_ref = fx.get("__schema")
    if not isinstance(schema_ref, str) or not schema_ref.strip():
        errs.append(Error(rel, 'Fixture must include "__schema": "<relative schema path>".'))
        return errs

    schema_path = (REPO_ROOT / schema_ref).resolve()
    if not schema_path.exists():
        errs.append(Error(rel, f'Fixture "__schema" does not exist: {schema_ref!r}'))
        return errs

    schema_key = str(schema_path.relative_to(REPO_ROOT)).replace("\\", "/")
    if schema_key not in schemas_cache:
        # Load schema (already validated structurally elsewhere)
        try:
            s = load_json(schema_path)
            if isinstance(s, dict):
                schemas_cache[schema_key] = s
            else:
                errs.append(Error(rel, f"Target schema is not an object: {schema_key}"))
                return errs
        except Exception as ex:
            errs.append(Error(rel, f"Failed to load target schema: {schema_key}: {ex}"))
            return errs

    schema_obj = schemas_cache[schema_key]
    required = schema_obj.get("required", [])
    if required is None:
        required = []
    if not isinstance(required, list):
        errs.append(Error(rel, f"Schema 'required' must be an array: {schema_key}"))
        return errs

    # Basic required field presence check
    for k in required:
        if isinstance(k, str):
            if k not in fx:
                errs.append(Error(rel, f"Missing required field {k!r} for schema {schema_key}"))
        else:
            errs.append(Error(rel, f"Invalid required entry in schema {schema_key}: {k!r}"))

    return errs


def iter_schema_files() -> List[Path]:
    files: List[Path] = []
    for d in SCHEMA_DIRS:
        if d.exists():
            files.extend(sorted(d.glob("*.schema.json")))
    return files


def iter_fixture_files() -> List[Path]:
    if not FIXTURES_DIR.exists():
        return []
    out: List[Path] = []
    for p in sorted(FIXTURES_DIR.glob("*.json")):
        if p.name == ".keep":
            continue
        out.append(p)
    return out


def main() -> int:
    errors: List[Error] = []
    schemas_cache: Dict[str, Dict[str, Any]] = {}

    schema_files = iter_schema_files()
    for sp in schema_files:
        errors.extend(validate_schema(sp))

    fixture_files = iter_fixture_files()
    for fp in fixture_files:
        errors.extend(validate_fixture(fp, schemas_cache))

    # Summary
    schemas_checked = len(schema_files)
    fixtures_checked = len(fixture_files)
    errors_count = len(errors)

    if errors_count:
        eprint("Contract validation FAILED")
        for err in errors:
            eprint(f"- {err.path}: {err.message}")
    else:
        print("Contract validation OK")

    print(f"schemas_checked={schemas_checked} fixtures_checked={fixtures_checked} errors_count={errors_count}")
    return 1 if errors_count else 0


if __name__ == "__main__":
    raise SystemExit(main())
