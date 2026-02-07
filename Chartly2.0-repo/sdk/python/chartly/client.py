from __future__ import annotations

from dataclasses import dataclass
import json
import secrets
from typing import Any
from urllib.parse import urljoin

try:
    import requests
    from requests.adapters import HTTPAdapter
except Exception as e:  # pragma: no cover
    raise ImportError(
        "The Chartly Python SDK requires 'requests'. Install with: pip install chartly-sdk"
    ) from e


DEFAULT_TENANT_HEADER = "X-Tenant-Id"
DEFAULT_REQUEST_ID_HEADER = "X-Request-Id"

DEFAULT_TIMEOUT_SECS = 15.0

DEFAULT_MAX_REQUEST_BYTES = 4 * 1024 * 1024   # 4 MiB
DEFAULT_MAX_RESPONSE_BYTES = 8 * 1024 * 1024  # 8 MiB

MAX_HEADER_KEY_LEN = 128
MAX_HEADER_VAL_LEN = 1024
MAX_EXTRA_HEADERS = 64

# Pool limits (no retries; thin SDK should not surprise)
DEFAULT_POOL_CONNECTIONS = 10
DEFAULT_POOL_MAXSIZE = 20


@dataclass(frozen=True)
class ErrorInfo:
    code: str
    message: str
    retryable: bool
    kind: str
    request_id: str = ""
    trace_id: str = ""


class APIError(RuntimeError):
    """
    Raised for non-2xx HTTP responses.

    Attempts to decode the Chartly error envelope:
      {"error":{"code","message","retryable","kind","request_id","trace_id"}}
    """

    def __init__(self, status: int, info: ErrorInfo, raw_body: bytes) -> None:
        self.status = int(status)
        self.info = info
        self.raw_body = raw_body
        super().__init__(self.__str__())

    def __str__(self) -> str:  # pragma: no cover
        code = self.info.code or "unknown"
        msg = self.info.message or "request failed"
        return f"Chartly API error: status={self.status} code={code} retryable={self.info.retryable} msg={msg}"


def _sanitize_header_value(v: str) -> str:
    """
    HTTP header values should be ASCII printable.
    Keep only 0x20..0x7E (space to tilde), excluding DEL.
    """
    v = (v or "").strip()
    if len(v) > MAX_HEADER_VAL_LEN:
        v = v[:MAX_HEADER_VAL_LEN]
    out_chars: list[str] = []
    for ch in v:
        o = ord(ch)
        if 0x20 <= o < 0x7F and o != 0x7F:
            out_chars.append(ch)
    return "".join(out_chars)


def _sanitize_headers(h: dict[str, str] | None) -> dict[str, str]:
    if not h:
        return {}
    out: dict[str, str] = {}
    for k, v in h.items():
        kk = (k or "").strip()
        if not kk or len(kk) > MAX_HEADER_KEY_LEN:
            continue
        if len(out) >= MAX_EXTRA_HEADERS:
            break
        out[kk] = _sanitize_header_value(str(v))
    return out


def _is_success(status: int) -> bool:
    return 200 <= int(status) <= 299


def _decode_error_envelope(status: int, raw: bytes) -> ErrorInfo:
    fallback = ErrorInfo(
        code="internal",
        message="request failed",
        retryable=True,
        kind="server",
    )
    if not raw:
        return fallback

    try:
        obj = json.loads(raw.decode("utf-8", errors="replace"))
    except Exception:
        return fallback

    if not isinstance(obj, dict):
        return fallback
    err = obj.get("error")
    if not isinstance(err, dict):
        return fallback

    code = str(err.get("code") or "internal").strip()[:128] or "internal"
    message = str(err.get("message") or "request failed")
    retryable = bool(err.get("retryable") if "retryable" in err else True)
    kind = str(err.get("kind") or "server").strip()[:64] or "server"
    request_id = str(err.get("request_id") or "")
    trace_id = str(err.get("trace_id") or "")

    message = _sanitize_header_value(message)[:512]
    request_id = _sanitize_header_value(request_id)[:128]
    trace_id = _sanitize_header_value(trace_id)[:128]

    return ErrorInfo(
        code=code,
        message=message,
        retryable=retryable,
        kind=kind,
        request_id=request_id,
        trace_id=trace_id,
    )


def generate_traceparent(sampled: bool = False) -> tuple[str, str, str]:
    """
    Generate a W3C traceparent header value.

    Returns: (traceparent, trace_id, span_id)
    Format: 00-<traceid32>-<spanid16>-<flags2>
    """
    # 16 bytes => 32 hex chars
    trace_id = secrets.token_hex(16)
    # 8 bytes => 16 hex chars
    span_id = secrets.token_hex(8)

    # If either is all-zero (astronomically unlikely), regenerate a few times.
    for _ in range(3):
        if trace_id != "0" * 32:
            break
        trace_id = secrets.token_hex(16)
    for _ in range(3):
        if span_id != "0" * 16:
            break
        span_id = secrets.token_hex(8)

    flags = "01" if sampled else "00"
    return f"00-{trace_id}-{span_id}-{flags}", trace_id, span_id


class Client:
    """
    Thin Chartly HTTP client.

    - Only provides helpers for /health and /ready
    - All other paths use request_json/request_raw
    - Sets tenancy/request-id headers consistently
    - Supports W3C trace propagation via traceparent/tracestate
    - Bounds request/response sizes for safety

    Session lifecycle:
    - If you pass a Session, you own it.
    - If Client creates a Session, Client will close it on close()/context exit.
    """

    def __init__(
        self,
        base_url: str,
        *,
        tenant_header: str = DEFAULT_TENANT_HEADER,
        request_id_header: str = DEFAULT_REQUEST_ID_HEADER,
        default_tenant: str = "",
        timeout_secs: float = DEFAULT_TIMEOUT_SECS,
        max_request_bytes: int = DEFAULT_MAX_REQUEST_BYTES,
        max_response_bytes: int = DEFAULT_MAX_RESPONSE_BYTES,
        session: requests.Session | None = None,
        static_headers: dict[str, str] | None = None,
        pool_connections: int = DEFAULT_POOL_CONNECTIONS,
        pool_maxsize: int = DEFAULT_POOL_MAXSIZE,
    ) -> None:
        base = (base_url or "").strip().rstrip("/")
        if not base:
            raise ValueError("base_url is required")
        self.base_url = base

        self.tenant_header = (tenant_header or DEFAULT_TENANT_HEADER).strip()
        self.request_id_header = (request_id_header or DEFAULT_REQUEST_ID_HEADER).strip()
        self.default_tenant = (default_tenant or "").strip()

        self.timeout_secs = float(timeout_secs) if timeout_secs else DEFAULT_TIMEOUT_SECS
        self.max_request_bytes = int(max_request_bytes) if max_request_bytes else DEFAULT_MAX_REQUEST_BYTES
        self.max_response_bytes = int(max_response_bytes) if max_response_bytes else DEFAULT_MAX_RESPONSE_BYTES

        self.static_headers = _sanitize_headers(static_headers)

        self._owned_session = False
        if session is None:
            session = requests.Session()
            self._owned_session = True
        self.session = session

        # Configure pooling; explicitly no retries here.
        adapter = HTTPAdapter(
            pool_connections=max(1, int(pool_connections)),
            pool_maxsize=max(1, int(pool_maxsize)),
            max_retries=0,
        )
        self.session.mount("http://", adapter)
        self.session.mount("https://", adapter)

    def __enter__(self) -> Client:
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        self.close()

    def close(self) -> None:
        if self._owned_session and self.session is not None:
            try:
                self.session.close()
            except Exception:
                pass

    def _build_url(self, path: str) -> str:
        p = (path or "/").strip()
        if not p.startswith("/"):
            p = "/" + p
        return urljoin(self.base_url + "/", p.lstrip("/"))

    def _build_headers(
        self,
        *,
        tenant_id: str = "",
        request_id: str = "",
        traceparent: str = "",
        tracestate: str = "",
        headers: dict[str, str] | None = None,
        content_type_json: bool = False,
    ) -> dict[str, str]:
        out: dict[str, str] = {}
        out.update(self.static_headers)
        out.update(_sanitize_headers(headers))

        # Core headers are authoritative (override user-provided if present)
        tid = (tenant_id or "").strip() or self.default_tenant
        if tid and self.tenant_header:
            out[self.tenant_header] = _sanitize_header_value(tid)

        rid = (request_id or "").strip()
        if rid and self.request_id_header:
            out[self.request_id_header] = _sanitize_header_value(rid)

        tp = (traceparent or "").strip()
        if tp:
            out["traceparent"] = _sanitize_header_value(tp)
        ts = (tracestate or "").strip()
        if ts:
            out["tracestate"] = _sanitize_header_value(ts)

        if content_type_json:
            out["Content-Type"] = "application/json"

        return out

    def request_raw(
        self,
        method: str,
        path: str,
        *,
        json_body: Any = None,
        tenant_id: str = "",
        request_id: str = "",
        traceparent: str = "",
        tracestate: str = "",
        headers: dict[str, str] | None = None,
        timeout_secs: float | None = None,
    ) -> bytes:
        """
        Perform an HTTP request and return raw response bytes (bounded).
        Raises APIError on non-2xx, with decoded Chartly error envelope if present.
        """
        m = (method or "").strip().upper()
        if not m:
            raise ValueError("method is required")

        url = self._build_url(path)

        body_bytes: bytes | None = None
        if json_body is not None and m not in ("GET", "HEAD"):
            # Deterministic JSON encoding helps idempotency friendliness.
            # NOTE: no default=str  non-serializable objects should raise TypeError.
            payload = json.dumps(
                json_body,
                ensure_ascii=False,
                separators=(",", ":"),
                sort_keys=True,
            ).encode("utf-8")
            if len(payload) > self.max_request_bytes:
                raise ValueError(f"request body too large ({len(payload)} > {self.max_request_bytes})")
            body_bytes = payload

        hdrs = self._build_headers(
            tenant_id=tenant_id,
            request_id=request_id,
            traceparent=traceparent,
            tracestate=tracestate,
            headers=headers,
            content_type_json=(body_bytes is not None),
        )

        to = float(timeout_secs) if timeout_secs is not None else self.timeout_secs

        resp = self.session.request(
            method=m,
            url=url,
            data=body_bytes,
            headers=hdrs,
            timeout=to,
            stream=True,
        )

        raw = self._read_bounded(resp, self.max_response_bytes)

        if _is_success(resp.status_code):
            return raw

        info = _decode_error_envelope(resp.status_code, raw)
        raise APIError(resp.status_code, info, raw)

    def request_json(
        self,
        method: str,
        path: str,
        *,
        json_body: Any = None,
        tenant_id: str = "",
        request_id: str = "",
        traceparent: str = "",
        tracestate: str = "",
        headers: dict[str, str] | None = None,
        timeout_secs: float | None = None,
    ) -> Any:
        raw = self.request_raw(
            method,
            path,
            json_body=json_body,
            tenant_id=tenant_id,
            request_id=request_id,
            traceparent=traceparent,
            tracestate=tracestate,
            headers=headers,
            timeout_secs=timeout_secs,
        )
        if not raw:
            return None
        return json.loads(raw.decode("utf-8", errors="replace"))

    def health(self, **kwargs: Any) -> bytes:
        return self.request_raw("GET", "/health", **kwargs)

    def ready(self, **kwargs: Any) -> bytes:
        return self.request_raw("GET", "/ready", **kwargs)

    @staticmethod
    def _read_bounded(resp: requests.Response, max_bytes: int) -> bytes:
        max_bytes = int(max_bytes)
        if max_bytes <= 0:
            max_bytes = DEFAULT_MAX_RESPONSE_BYTES

        buf = bytearray()
        try:
            for chunk in resp.iter_content(chunk_size=64 * 1024):
                if not chunk:
                    continue
                buf.extend(chunk)
                if len(buf) > max_bytes:
                    resp.close()
                    raise ValueError(f"response body too large ({len(buf)} > {max_bytes})")
        finally:
            resp.close()

        return bytes(buf)
