#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

TOOL_VERSION = "0.1.0"

# Exit codes (tools/README.md contract)
EXIT_SUCCESS = 0
EXIT_GENERAL_ERROR = 1
EXIT_INVALID_ARGS = 2
EXIT_PRECONDITION_FAILED = 3
EXIT_VALIDATION_FAILED = 4
EXIT_UNSAFE_BLOCKED = 5

SECRET_KEY_PATTERNS = (
    "token",
    "password",
    "secret",
    "authorization",
    "private_key",
)

# Simple heuristic for secret-ish values (bounded; deterministic)
SECRET_VALUE_RE = re.compile(r"(bearer\s+[a-z0-9\-\._]+|-----begin\s+private\s+key-----)", re.IGNORECASE)


@dataclass(frozen=True)
class Action:
    rel_path: str
    kind: str  # openapi | schema | manifest | hash
    content_bytes: bytes
    sha256: str


def _summary_line(status: str, code: int, duration_ms: int) -> str:
    return f"{status} code={code} duration_ms={duration_ms}"


def _print_err(msg: str) -> None:
    sys.stderr.write(msg + "\n")


def _sha256_hex(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def _canonical_json_bytes(obj: Any, pretty: bool = False) -> bytes:
    # Deterministic encoding:
    # - sort_keys=True
    # - stable separators
    # - ensure_ascii=False for stable UTF-8
    if pretty:
        s = json.dumps(obj, sort_keys=True, indent=2, ensure_ascii=False)
    else:
        s = json.dumps(obj, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    return (s + "\n").encode("utf-8")


def _load_json_file(path: Path) -> Any:
    data = path.read_bytes()
    try:
        return json.loads(data.decode("utf-8"))
    except Exception as e:
        raise ValueError(f"invalid_json:{str(path)}") from e


def _is_yaml_path(path: Path) -> bool:
    return path.suffix.lower() in (".yaml", ".yml")


def _walk_files(root: Path, include: Optional[str], exclude: Optional[str]) -> List[Path]:
    # Deterministic file discovery (sorted)
    files: List[Path] = []
    if not root.exists():
        return files
    inc_re = re.compile(include) if include else None
    exc_re = re.compile(exclude) if exclude else None

    for p in sorted(root.rglob("*")):
        if p.is_dir():
            continue
        rel = str(p.relative_to(root)).replace("\\", "/")
        if inc_re and not inc_re.search(rel):
            continue
        if exc_re and exc_re.search(rel):
            continue
        files.append(p)
    return files


def _detect_secrets_in_obj(obj: Any) -> List[str]:
    findings: List[str] = []

    def walk(x: Any, path: str) -> None:
        if isinstance(x, dict):
            for k, v in x.items():
                k_str = str(k)
                low = k_str.lower()
                for pat in SECRET_KEY_PATTERNS:
                    if pat in low:
                        findings.append(f"secret_like_key:{path}/{k_str}")
                        break
                walk(v, f"{path}/{k_str}")
        elif isinstance(x, list):
            for i, v in enumerate(x):
                walk(v, f"{path}[{i}]")
        elif isinstance(x, str):
            if SECRET_VALUE_RE.search(x):
                findings.append(f"secret_like_value:{path}")
        else:
            return

    walk(obj, "$")
    findings.sort()
    return findings


def _redact_obj(obj: Any) -> Any:
    # Deterministic redaction:
    # - secret-like keys => "<redacted>"
    # - secret-like string values => "<redacted>"
    def redact(x: Any) -> Any:
        if isinstance(x, dict):
            out: Dict[str, Any] = {}
            for k in sorted(x.keys(), key=lambda z: str(z)):
                v = x[k]
                k_str = str(k)
                low = k_str.lower()
                if any(pat in low for pat in SECRET_KEY_PATTERNS):
                    out[k_str] = "<redacted>"
                else:
                    out[k_str] = redact(v)
            return out
        if isinstance(x, list):
            return [redact(v) for v in x]
        if isinstance(x, str):
            if SECRET_VALUE_RE.search(x):
                return "<redacted>"
            return x
        return x

    return redact(obj)


def _normalize_openapi_json(openapi_obj: Any, include_vendor_extensions: bool = False) -> Any:
    """
    Deterministic OpenAPI canonicalization (bounded, provider-neutral):
    - stable key ordering via canonical JSON output
    - remove examples
    - remove x-* extensions unless include_vendor_extensions=True
    - preserve descriptions (consistent behavior)
    """

    def norm(x: Any) -> Any:
        if isinstance(x, dict):
            out: Dict[str, Any] = {}
            for k in sorted(x.keys(), key=lambda z: str(z)):
                ks = str(k)
                if ks == "examples":
                    continue
                if ks.startswith("x-") and not include_vendor_extensions:
                    continue
                out[ks] = norm(x[k])
            return out
        if isinstance(x, list):
            return [norm(v) for v in x]
        return x

    return norm(openapi_obj)


def _secret_failure_message(findings: List[str]) -> str:
    # Deterministic minimal reporting:
    # - count
    # - first finding (already sorted)
    if not findings:
        return "secret_detection_failed count=0"
    return f"secret_detection_failed count={len(findings)} first={findings[0]}"


def _build_actions(
    repo_root: Path,
    out_root: Path,
    openapi_path: Optional[Path],
    schemas_root: Optional[Path],
    include: Optional[str],
    exclude: Optional[str],
    redact: bool,
) -> Tuple[List[Action], Dict[str, Any]]:
    """
    Build deterministic in-memory actions (no writes):
    - openapi normalized json (JSON input only; YAML must be rejected before calling)
    - schemas copied + canonicalized (JSON only; YAML must be rejected before calling)
    - manifest + sha256 files
    """
    actions: List[Action] = []

    inputs: List[Tuple[str, str]] = []  # (kind, relpath)
    input_hashes: Dict[str, str] = {}

    def add_input(kind: str, p: Path) -> None:
        rel = str(p.relative_to(repo_root)).replace("\\", "/")
        inputs.append((kind, rel))
        input_hashes[rel] = _sha256_hex(p.read_bytes())

    # OpenAPI (JSON only)
    if openapi_path:
        p = openapi_path
        if not p.is_absolute():
            p = (repo_root / p).resolve()
        if not p.exists():
            raise FileNotFoundError(f"missing openapi input: {openapi_path}")
        add_input("openapi", p)

        o = _load_json_file(p)
        sec = _detect_secrets_in_obj(o)
        if sec and not redact:
            raise ValueError("secret_detection_failed:" + ";".join(sec))
        if redact and sec:
            o = _redact_obj(o)

        normalized = _normalize_openapi_json(o)
        b = _canonical_json_bytes(normalized, pretty=True)
        rel_out = "openapi/openapi.normalized.json"
        actions.append(Action(rel_path=rel_out, kind="openapi", content_bytes=b, sha256=_sha256_hex(b)))

    # Schemas (JSON only)
    schema_outputs: List[str] = []
    if schemas_root:
        root = schemas_root
        if not root.is_absolute():
            root = (repo_root / root).resolve()
        if not root.exists():
            raise FileNotFoundError(f"missing schemas root: {schemas_root}")

        files = _walk_files(root, include, exclude)
        for f in files:
            add_input("schema", f)
            obj = _load_json_file(f)
            sec = _detect_secrets_in_obj(obj)
            if sec and not redact:
                raise ValueError("secret_detection_failed:" + ";".join(sec))
            if redact and sec:
                obj = _redact_obj(obj)

            rel_under = str(f.relative_to(root)).replace("\\", "/")
            rel_out = f"schemas/{rel_under}"
            b = _canonical_json_bytes(obj, pretty=True)
            actions.append(Action(rel_path=rel_out, kind="schema", content_bytes=b, sha256=_sha256_hex(b)))
            schema_outputs.append(rel_out)

    # Manifest (deterministic)
    inputs_sorted = sorted(inputs, key=lambda t: (t[0], t[1]))
    manifest_obj: Dict[str, Any] = {
        "tool": {"name": "schema-gen", "version": TOOL_VERSION},
        "inputs": [{"kind": k, "path": p, "sha256": input_hashes.get(p, "")} for (k, p) in inputs_sorted],
        "outputs": [],
        "redaction": {"enabled": bool(redact)},
    }

    for a in sorted(actions, key=lambda x: x.rel_path):
        manifest_obj["outputs"].append({"kind": a.kind, "path": a.rel_path, "sha256": a.sha256})

    manifest_bytes = _canonical_json_bytes(manifest_obj, pretty=True)
    manifest_sha = _sha256_hex(manifest_bytes)
    actions.append(Action(rel_path="manifest.json", kind="manifest", content_bytes=manifest_bytes, sha256=manifest_sha))

    # Hash files (deterministic)
    def add_hash(rel: str, value: str) -> None:
        b = (value + "\n").encode("utf-8")
        actions.append(Action(rel_path=rel, kind="hash", content_bytes=b, sha256=_sha256_hex(b)))

    add_hash("hashes/manifest.sha256", manifest_sha)

    openapi_action = next((a for a in actions if a.kind == "openapi"), None)
    if openapi_action is not None:
        add_hash("hashes/openapi.normalized.sha256", openapi_action.sha256)

    schema_lines = []
    for a in sorted(actions, key=lambda x: x.rel_path):
        if a.kind == "schema":
            schema_lines.append(f"{a.sha256}  {a.rel_path}")
    schemas_index = "\n".join(schema_lines) + ("\n" if schema_lines else "")
    add_hash("hashes/schemas.sha256", _sha256_hex(schemas_index.encode("utf-8")))

    meta = {
        "inputs_count": len(inputs_sorted),
        "outputs_count": len(actions),
        "schemas_count": len(schema_outputs),
        "openapi_present": bool(openapi_path),
        "redaction_enabled": bool(redact),
    }
    return actions, meta


def _write_actions(out_root: Path, actions: List[Action]) -> None:
    for a in sorted(actions, key=lambda x: x.rel_path):
        p = out_root / a.rel_path
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_bytes(a.content_bytes)


def _load_existing_bytes(out_root: Path, rel_path: str) -> Optional[bytes]:
    p = out_root / rel_path
    if not p.exists():
        return None
    return p.read_bytes()


def _reject_yaml_inputs(repo_root: Path, openapi_path: Optional[Path], schemas_root: Optional[Path],
                        include: Optional[str], exclude: Optional[str]) -> Optional[str]:
    # Enforce YAML rejection deterministically for both OpenAPI and schemas before building actions.
    if openapi_path:
        p = openapi_path if openapi_path.is_absolute() else (repo_root / openapi_path)
        if _is_yaml_path(p):
            return "openapi_yaml_not_supported"
    if schemas_root:
        root = schemas_root if schemas_root.is_absolute() else (repo_root / schemas_root)
        if root.exists():
            for f in _walk_files(root, include, exclude):
                if _is_yaml_path(f):
                    return "schemas_yaml_not_supported"
    return None


def _cmd_plan(cfg: argparse.Namespace) -> int:
    repo_root = Path(cfg.path).resolve()
    out_root = (repo_root / cfg.out).resolve()
    openapi_path = Path(cfg.openapi) if cfg.openapi else None
    schemas_root = Path(cfg.schemas) if cfg.schemas else None

    yaml_reason = _reject_yaml_inputs(repo_root, openapi_path, schemas_root, cfg.include, cfg.exclude)
    if yaml_reason:
        _print_err(f"FAILED code=3 msg={yaml_reason}")
        return EXIT_PRECONDITION_FAILED

    try:
        actions, meta = _build_actions(repo_root, out_root, openapi_path, schemas_root,
                                       cfg.include, cfg.exclude, cfg.redact)
    except FileNotFoundError as e:
        _print_err(f"FAILED code=3 msg={str(e)}")
        return EXIT_PRECONDITION_FAILED
    except ValueError as e:
        msg = str(e)
        if msg.startswith("secret_detection_failed:"):
            findings = msg.split(":", 1)[1].split(";") if ":" in msg else []
            _print_err(f"FAILED code=4 msg={_secret_failure_message(sorted([f for f in findings if f]))}")
            return EXIT_VALIDATION_FAILED
        _print_err(f"FAILED code=4 msg={msg}")
        return EXIT_VALIDATION_FAILED
    except Exception as e:
        _print_err(f"FAILED code=1 msg={type(e).__name__}")
        return EXIT_GENERAL_ERROR

    out_obj = {
        "tool": {"name": "schema-gen", "version": TOOL_VERSION},
        "mode": "plan",
        "path": str(repo_root).replace("\\", "/"),
        "out": str(out_root).replace("\\", "/"),
        "meta": meta,
        "actions": [{"path": a.rel_path, "kind": a.kind, "sha256": a.sha256, "bytes": len(a.content_bytes)}
                    for a in sorted(actions, key=lambda x: x.rel_path)],
    }
    if cfg.format == "json":
        sys.stdout.write(_canonical_json_bytes(out_obj, pretty=True).decode("utf-8"))
    else:
        sys.stdout.write(f"schema-gen plan: actions={len(actions)} inputs={meta['inputs_count']} outputs={meta['outputs_count']}\n")
    return EXIT_SUCCESS


def _cmd_generate(cfg: argparse.Namespace) -> int:
    if not cfg.apply:
        _print_err("FAILED code=5 msg=unsafe_operation_blocked_missing_apply")
        return EXIT_UNSAFE_BLOCKED

    repo_root = Path(cfg.path).resolve()
    out_root = (repo_root / cfg.out).resolve()
    openapi_path = Path(cfg.openapi) if cfg.openapi else None
    schemas_root = Path(cfg.schemas) if cfg.schemas else None

    yaml_reason = _reject_yaml_inputs(repo_root, openapi_path, schemas_root, cfg.include, cfg.exclude)
    if yaml_reason:
        _print_err(f"FAILED code=3 msg={yaml_reason}")
        return EXIT_PRECONDITION_FAILED

    try:
        actions, _ = _build_actions(repo_root, out_root, openapi_path, schemas_root,
                                    cfg.include, cfg.exclude, cfg.redact)
    except FileNotFoundError as e:
        _print_err(f"FAILED code=3 msg={str(e)}")
        return EXIT_PRECONDITION_FAILED
    except ValueError as e:
        msg = str(e)
        if msg.startswith("secret_detection_failed:"):
            findings = msg.split(":", 1)[1].split(";") if ":" in msg else []
            _print_err(f"FAILED code=4 msg={_secret_failure_message(sorted([f for f in findings if f]))}")
            return EXIT_VALIDATION_FAILED
        _print_err(f"FAILED code=4 msg={msg}")
        return EXIT_VALIDATION_FAILED
    except Exception as e:
        _print_err(f"FAILED code=1 msg={type(e).__name__}")
        return EXIT_GENERAL_ERROR

    if cfg.dry_run:
        out_obj = {
            "tool": {"name": "schema-gen", "version": TOOL_VERSION},
            "mode": "generate",
            "dry_run": True,
            "out": str(out_root).replace("\\", "/"),
            "writes": [{"path": a.rel_path, "kind": a.kind, "sha256": a.sha256} for a in sorted(actions, key=lambda x: x.rel_path)],
        }
        if cfg.format == "json":
            sys.stdout.write(_canonical_json_bytes(out_obj, pretty=True).decode("utf-8"))
        else:
            sys.stdout.write(f"schema-gen generate --dry-run: writes={len(actions)}\n")
        return EXIT_SUCCESS

    _write_actions(out_root, actions)

    out_obj = {
        "tool": {"name": "schema-gen", "version": TOOL_VERSION},
        "mode": "generate",
        "dry_run": False,
        "out": str(out_root).replace("\\", "/"),
        "written": [{"path": a.rel_path, "kind": a.kind, "sha256": a.sha256} for a in sorted(actions, key=lambda x: x.rel_path)],
    }
    if cfg.format == "json":
        sys.stdout.write(_canonical_json_bytes(out_obj, pretty=True).decode("utf-8"))
    else:
        sys.stdout.write(f"schema-gen generate: written={len(actions)}\n")
    return EXIT_SUCCESS


def _cmd_verify(cfg: argparse.Namespace) -> int:
    repo_root = Path(cfg.path).resolve()
    out_root = (repo_root / cfg.out).resolve()
    openapi_path = Path(cfg.openapi) if cfg.openapi else None
    schemas_root = Path(cfg.schemas) if cfg.schemas else None

    yaml_reason = _reject_yaml_inputs(repo_root, openapi_path, schemas_root, cfg.include, cfg.exclude)
    if yaml_reason:
        _print_err(f"FAILED code=3 msg={yaml_reason}")
        return EXIT_PRECONDITION_FAILED

    # Explicit output root precondition (clear operator UX)
    if not out_root.exists():
        out_obj = {"ok": False, "code": "precondition_failed", "message": "missing_output_root", "out": str(out_root).replace("\\", "/")}
        if cfg.format == "json":
            sys.stdout.write(_canonical_json_bytes(out_obj, pretty=True).decode("utf-8"))
        else:
            sys.stdout.write("verify precondition_failed missing_output_root\n")
        return EXIT_PRECONDITION_FAILED

    try:
        actions, _ = _build_actions(repo_root, out_root, openapi_path, schemas_root,
                                    cfg.include, cfg.exclude, cfg.redact)
    except FileNotFoundError as e:
        _print_err(f"FAILED code=3 msg={str(e)}")
        return EXIT_PRECONDITION_FAILED
    except ValueError as e:
        msg = str(e)
        if msg.startswith("secret_detection_failed:"):
            findings = msg.split(":", 1)[1].split(";") if ":" in msg else []
            _print_err(f"FAILED code=4 msg={_secret_failure_message(sorted([f for f in findings if f]))}")
            return EXIT_VALIDATION_FAILED
        _print_err(f"FAILED code=4 msg={msg}")
        return EXIT_VALIDATION_FAILED
    except Exception as e:
        _print_err(f"FAILED code=1 msg={type(e).__name__}")
        return EXIT_GENERAL_ERROR

    mismatches: List[Dict[str, str]] = []
    missing: List[str] = []

    for a in sorted(actions, key=lambda x: x.rel_path):
        existing = _load_existing_bytes(out_root, a.rel_path)
        if existing is None:
            missing.append(a.rel_path)
            continue
        if _sha256_hex(existing) != a.sha256:
            mismatches.append({"path": a.rel_path, "expected_sha256": a.sha256, "actual_sha256": _sha256_hex(existing)})

    if missing:
        out_obj = {"ok": False, "code": "precondition_failed", "missing": sorted(missing)}
        if cfg.format == "json":
            sys.stdout.write(_canonical_json_bytes(out_obj, pretty=True).decode("utf-8"))
        else:
            sys.stdout.write(f"verify precondition_failed missing={len(missing)}\n")
        return EXIT_PRECONDITION_FAILED

    if mismatches:
        out_obj = {"ok": False, "code": "validation_failed", "mismatches": mismatches}
        if cfg.format == "json":
            sys.stdout.write(_canonical_json_bytes(out_obj, pretty=True).decode("utf-8"))
        else:
            sys.stdout.write(f"verify validation_failed mismatches={len(mismatches)}\n")
        return EXIT_VALIDATION_FAILED

    out_obj = {"ok": True, "code": "ok"}
    if cfg.format == "json":
        sys.stdout.write(_canonical_json_bytes(out_obj, pretty=True).decode("utf-8"))
    else:
        sys.stdout.write("verify ok\n")
    return EXIT_SUCCESS


def main(argv: Optional[List[str]] = None) -> int:
    start = time.monotonic()
    argv = list(sys.argv[1:] if argv is None else argv)

    p = argparse.ArgumentParser(prog="schema-gen", add_help=True)
    sub = p.add_subparsers(dest="command")

    def add_common(sp: argparse.ArgumentParser) -> None:
        sp.add_argument("--path", required=True, help="Repo root or contracts root")
        sp.add_argument("--out", default="contracts", help="Output root relative to --path (default: contracts/)")
        sp.add_argument("--openapi", default="", help="OpenAPI input path (JSON supported; YAML roadmap)")
        sp.add_argument("--schemas", default="", help="Schema input root directory")
        sp.add_argument("--include", default="", help="Regex include filter (applies under --schemas)")
        sp.add_argument("--exclude", default="", help="Regex exclude filter (applies under --schemas)")
        sp.add_argument("--format", default="json", choices=["json", "text"], help="Output format")
        sp.add_argument("--redact", action="store_true", help="Opt-in deterministic redaction (default is fail-fast)")

    sp_plan = sub.add_parser("plan")
    add_common(sp_plan)

    sp_gen = sub.add_parser("generate")
    add_common(sp_gen)
    sp_gen.add_argument("--apply", action="store_true", help="Required to write outputs")
    sp_gen.add_argument("--dry-run", action="store_true", help="Plan writes without writing")

    sp_verify = sub.add_parser("verify")
    add_common(sp_verify)

    try:
        ns = p.parse_args(argv)
    except SystemExit:
        duration_ms = int((time.monotonic() - start) * 1000)
        _print_err(_summary_line("FAILED", EXIT_INVALID_ARGS, duration_ms))
        return EXIT_INVALID_ARGS

    cmd = (ns.command or "").strip().lower()
    if cmd not in ("plan", "generate", "verify"):
        duration_ms = int((time.monotonic() - start) * 1000)
        _print_err(_summary_line("FAILED", EXIT_INVALID_ARGS, duration_ms))
        return EXIT_INVALID_ARGS

    # Normalize empty string args
    ns.openapi = ns.openapi.strip()
    ns.schemas = ns.schemas.strip()
    ns.include = ns.include.strip() or None
    ns.exclude = ns.exclude.strip() or None

    # Execute
    if cmd == "plan":
        rc = _cmd_plan(ns)
    elif cmd == "generate":
        rc = _cmd_generate(ns)
    else:
        rc = _cmd_verify(ns)

    duration_ms = int((time.monotonic() - start) * 1000)
    if rc == 0:
        _print_err(_summary_line("OK", rc, duration_ms))
    else:
        _print_err(_summary_line("FAILED", rc, duration_ms))
    return rc


if __name__ == "__main__":
    sys.exit(main())
