from __future__ import annotations

import argparse
import json
import sys
from typing import Any

try:
    # Lazy export in chartly/__init__.py supports this.
    from chartly import Client
    from chartly.client import APIError, generate_traceparent
except ImportError as e:
    print(f"chartly package not available: {e}", file=sys.stderr)
    print("Install (editable) from repo root:", file=sys.stderr)
    print("  pip install -e C:\\Chartly2.0\\sdk\\python", file=sys.stderr)
    raise SystemExit(2)

# Optional: structured parsing if models.py exists
try:
    from chartly.models import parse_health_snapshot  # type: ignore
except Exception:  # pragma: no cover
    parse_health_snapshot = None  # type: ignore


def _print_section(title: str) -> None:
    print("\n" + title)
    print("-" * len(title))


def _try_decode_json(raw: bytes) -> Any:
    if not raw:
        return None
    try:
        return json.loads(raw.decode("utf-8", errors="replace"))
    except Exception:
        return None


def main() -> int:
    p = argparse.ArgumentParser(description="Chartly Python SDK basic client example")
    p.add_argument("--base", default="http://localhost:8080", help="Base URL (typically gateway)")
    p.add_argument("--tenant", default="local", help="Tenant id (X-Tenant-Id)")
    p.add_argument("--request", default="req_python_basic_client", help="Request id (X-Request-Id)")
    p.add_argument("--timeout", type=float, default=10.0, help="Timeout seconds")
    p.add_argument("--trace", action="store_true", help="Generate and send a W3C traceparent")
    args = p.parse_args()

    traceparent = ""
    if args.trace:
        traceparent, trace_id, span_id = generate_traceparent(sampled=False)
        print("trace_id:", trace_id)
        print("span_id :", span_id)

    try:
        with Client(args.base, default_tenant=args.tenant, timeout_secs=args.timeout) as c:
            _print_section("/health")
            raw = c.health(request_id=args.request, traceparent=traceparent)
            txt = raw.decode("utf-8", errors="replace")
            print(txt)

            obj = _try_decode_json(raw)
            if isinstance(obj, dict):
                # Prefer structured model parsing if available
                if parse_health_snapshot is not None:
                    try:
                        hs = parse_health_snapshot(obj)
                        print("\nparsed:")
                        print("service    :", hs.service)
                        print("overall    :", hs.overall)
                        print("hash       :", hs.hash)
                        print("components :", len(hs.components))
                    except Exception:
                        # Fallback to minimal fields
                        print("\nparsed (minimal):")
                        print("service:", obj.get("service"))
                        print("overall:", obj.get("overall"))
                        print("hash   :", obj.get("hash"))

            _print_section("/ready")
            raw = c.ready(request_id=args.request, traceparent=traceparent)
            print(raw.decode("utf-8", errors="replace"))

    except APIError as e:
        print("\nAPI Error:", file=sys.stderr)
        print(f"  status    : {e.status}", file=sys.stderr)
        print(f"  code      : {e.info.code}", file=sys.stderr)
        print(f"  retryable : {e.info.retryable}", file=sys.stderr)
        if e.info.request_id:
            print(f"  request_id: {e.info.request_id}", file=sys.stderr)
        if e.info.trace_id:
            print(f"  trace_id  : {e.info.trace_id}", file=sys.stderr)
        if e.info.message:
            print(f"  message   : {e.info.message}", file=sys.stderr)
        return 1
    except Exception as e:
        print(f"\nUnexpected error: {e}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
