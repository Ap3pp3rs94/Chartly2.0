from __future__ import annotations

import json
import os
import sys
from typing import Iterable


def iter_schema_files(root: str) -> Iterable[str]:
    for dirpath, _dirnames, filenames in os.walk(root):
        for name in filenames:
            if name.endswith(".schema.json"):
                yield os.path.join(dirpath, name)


def validate_schema(path: str) -> tuple[bool, str]:
    try:
        with open(path, "rb") as f:
            obj = json.loads(f.read().decode("utf-8"))
    except Exception as e:  # pragma: no cover
        return False, f"invalid JSON: {e}"

    if not isinstance(obj, dict):
        return False, "schema must be a JSON object"
    if "$schema" not in obj or "$id" not in obj:
        return False, "missing $schema or $id"
    return True, "ok"


def main() -> int:
    root = os.environ.get("CHARTLY_SCHEMA_ROOT", os.path.join(os.getcwd(), "contracts"))
    if not os.path.isdir(root):
        print(f"schema root not found: {root}", file=sys.stderr)
        return 2

    files = sorted(iter_schema_files(root))
    if not files:
        print("no schema files found", file=sys.stderr)
        return 1

    bad = 0
    for p in files:
        ok, msg = validate_schema(p)
        rel = os.path.relpath(p, os.getcwd())
        if ok:
            print(f"OK   {rel}")
        else:
            bad += 1
            print(f"FAIL {rel}  ({msg})")

    if bad:
        print(f"{bad} schema file(s) invalid", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
