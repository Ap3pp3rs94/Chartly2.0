from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Mapping, Sequence, Literal


# ---------------------------------------------------------------------------
# Shared bounds (defense-in-depth)
# ---------------------------------------------------------------------------

MAX_STR_SERVICE = 64
MAX_STR_ENV = 32
MAX_STR_TENANT = 64

MAX_COMPONENTS = 64
MAX_COMPONENT_NAME = 64
MAX_COMPONENT_MESSAGE = 256

MAX_DETAILS = 32
MAX_DETAIL_KEY = 64
MAX_DETAIL_VAL = 256

MAX_WARNINGS = 32
MAX_WARNING_CODE = 64
MAX_WARNING_SUBJECT = 64
MAX_WARNING_MESSAGE = 256

MAX_ERROR_CODE = 128
MAX_ERROR_KIND = 64
MAX_ERROR_MESSAGE = 512
MAX_ID = 128

MAX_ERROR_DETAILS = 32
MAX_ERROR_DETAIL_KEY = 64
MAX_ERROR_DETAIL_VAL = 256


HealthStatus = Literal["ok", "degraded", "fatal", "unknown"]


def _trim(s: str, max_len: int) -> str:
    s = (s or "").strip()
    if len(s) > max_len:
        s = s[:max_len]
    # Strip ASCII control chars (keep printable + UTF-8 visible; not for headers).
    return "".join(ch for ch in s if ord(ch) >= 0x20 and ord(ch) != 0x7F)


def _as_str(v: Any, max_len: int, default: str = "") -> str:
    if v is None:
        return default
    return _trim(str(v), max_len)


def _as_bool(v: Any, default: bool = False) -> bool:
    if v is None:
        return default
    if isinstance(v, bool):
        return v
    # permissive but deterministic
    if isinstance(v, (int, float)):
        return bool(v)
    if isinstance(v, str):
        s = v.strip().lower()
        if s in ("1", "true", "yes", "y", "on"):
            return True
        if s in ("0", "false", "no", "n", "off"):
            return False
    return default


def _as_status(v: Any) -> HealthStatus:
    s = _as_str(v, 16, default="unknown").lower()
    if s in ("ok", "degraded", "fatal", "unknown"):
        return s  # type: ignore[return-value]
    return "unknown"  # type: ignore[return-value]


def _as_dict_str_str(v: Any, *, max_items: int, max_k: int, max_v: int, lower_keys: bool) -> dict[str, str]:
    if v is None:
        return {}
    if not isinstance(v, Mapping):
        raise ValueError("expected mapping for details/extra fields")
    out: dict[str, str] = {}
    # deterministic key order
    keys = sorted([str(k) for k in v.keys()])
    for k in keys:
        if len(out) >= max_items:
            break
        kk = _trim(k, max_k)
        if lower_keys:
            kk = kk.lower()
        if not kk:
            continue
        vv = _as_str(v.get(k), max_v, default="")
        out[kk] = vv
    return out


# ---------------------------------------------------------------------------
# Health models (aligned with pkg/telemetry/health.go)
# ---------------------------------------------------------------------------

@dataclass(frozen=True)
class HealthWarning:
    code: str
    subject: str = ""
    message: str = ""

    @staticmethod
    def from_obj(obj: Any) -> HealthWarning:
        if not isinstance(obj, Mapping):
            raise ValueError("HealthWarning must be an object")
        return HealthWarning(
            code=_as_str(obj.get("code"), MAX_WARNING_CODE, default=""),
            subject=_as_str(obj.get("subject"), MAX_WARNING_SUBJECT, default=""),
            message=_as_str(obj.get("message"), MAX_WARNING_MESSAGE, default=""),
        )


@dataclass(frozen=True)
class ComponentStatus:
    name: str
    status: HealthStatus
    checked_at: str  # RFC3339 expected from services; kept as string for portability
    message: str = ""
    details: dict[str, str] = field(default_factory=dict)

    @staticmethod
    def from_obj(obj: Any) -> ComponentStatus:
        if not isinstance(obj, Mapping):
            raise ValueError("ComponentStatus must be an object")
        name = _as_str(obj.get("name"), MAX_COMPONENT_NAME, default="")
        if not name:
            raise ValueError("ComponentStatus.name is required")
        status = _as_status(obj.get("status"))
        checked_at = _as_str(obj.get("checked_at"), 64, default="")
        if not checked_at:
            raise ValueError("ComponentStatus.checked_at is required")
        message = _as_str(obj.get("message"), MAX_COMPONENT_MESSAGE, default="")
        details = _as_dict_str_str(
            obj.get("details"),
            max_items=MAX_DETAILS,
            max_k=MAX_DETAIL_KEY,
            max_v=MAX_DETAIL_VAL,
            lower_keys=True,
        ) if obj.get("details") is not None else {}
        return ComponentStatus(
            name=name,
            status=status,
            checked_at=checked_at,
            message=message,
            details=details,
        )


@dataclass(frozen=True)
class HealthSnapshot:
    service: str
    generated_at: str
    overall: HealthStatus
    hash: str

    env: str = ""
    tenant: str = ""
    components: tuple[ComponentStatus, ...] = field(default_factory=tuple)
    warnings: tuple[HealthWarning, ...] = field(default_factory=tuple)

    def normalized(self) -> HealthSnapshot:
        """
        Returns a copy with deterministic ordering:
        - components sorted by name (case-insensitive)
        - details dict keys already normalized by parsing
        - warnings sorted by code,subject,message

        Does NOT recompute or change the service-provided `hash`.
        """
        comps = tuple(sorted(self.components, key=lambda c: c.name.lower()))
        warns = tuple(sorted(self.warnings, key=lambda w: (w.code, w.subject, w.message)))
        return HealthSnapshot(
            service=self.service,
            env=self.env,
            tenant=self.tenant,
            generated_at=self.generated_at,
            overall=self.overall,
            components=comps,
            hash=self.hash,
            warnings=warns,
        )

    @staticmethod
    def from_obj(obj: Any) -> HealthSnapshot:
        if not isinstance(obj, Mapping):
            raise ValueError("HealthSnapshot must be an object")
        service = _as_str(obj.get("service"), MAX_STR_SERVICE, default="")
        if not service:
            raise ValueError("HealthSnapshot.service is required")
        env = _as_str(obj.get("env"), MAX_STR_ENV, default="")
        tenant = _as_str(obj.get("tenant"), MAX_STR_TENANT, default="")
        generated_at = _as_str(obj.get("generated_at"), 64, default="")
        if not generated_at:
            raise ValueError("HealthSnapshot.generated_at is required")
        overall = _as_status(obj.get("overall"))
        hsh = _as_str(obj.get("hash"), 128, default="")
        if not hsh:
            raise ValueError("HealthSnapshot.hash is required")

        comps_raw = obj.get("components")
        comps: list[ComponentStatus] = []
        if comps_raw is not None:
            if not isinstance(comps_raw, Sequence) or isinstance(comps_raw, (str, bytes)):
                raise ValueError("HealthSnapshot.components must be an array")
            for item in comps_raw[:MAX_COMPONENTS]:
                comps.append(ComponentStatus.from_obj(item))

        warns_raw = obj.get("warnings")
        warns: list[HealthWarning] = []
        if warns_raw is not None:
            if not isinstance(warns_raw, Sequence) or isinstance(warns_raw, (str, bytes)):
                raise ValueError("HealthSnapshot.warnings must be an array")
            for item in warns_raw[:MAX_WARNINGS]:
                warns.append(HealthWarning.from_obj(item))

        return HealthSnapshot(
            service=service,
            env=env,
            tenant=tenant,
            generated_at=generated_at,
            overall=overall,
            components=tuple(comps),
            hash=hsh,
            warnings=tuple(warns),
        )


def parse_health_snapshot(obj: Any) -> HealthSnapshot:
    """
    Parse a HealthSnapshot from decoded JSON (dict).
    Raises ValueError on invalid shape.
    """
    return HealthSnapshot.from_obj(obj)


# ---------------------------------------------------------------------------
# Error envelope models (aligned with pkg/errors envelope shape)
# ---------------------------------------------------------------------------

@dataclass(frozen=True)
class KV:
    k: str
    v: str

    @staticmethod
    def from_obj(obj: Any) -> KV:
        if not isinstance(obj, Mapping):
            raise ValueError("KV must be an object")
        return KV(
            k=_as_str(obj.get("k"), MAX_ERROR_DETAIL_KEY, default=""),
            v=_as_str(obj.get("v"), MAX_ERROR_DETAIL_VAL, default=""),
        )


@dataclass(frozen=True)
class ErrorBody:
    code: str
    message: str
    retryable: bool
    kind: str

    request_id: str = ""
    trace_id: str = ""
    details: tuple[KV, ...] = field(default_factory=tuple)

    @staticmethod
    def from_obj(obj: Any) -> ErrorBody:
        if not isinstance(obj, Mapping):
            raise ValueError("error body must be an object")
        code = _as_str(obj.get("code"), MAX_ERROR_CODE, default="internal")
        message = _as_str(obj.get("message"), MAX_ERROR_MESSAGE, default="request failed")
        retryable = _as_bool(obj.get("retryable"), default=True)
        kind = _as_str(obj.get("kind"), MAX_ERROR_KIND, default="server")

        request_id = _as_str(obj.get("request_id"), MAX_ID, default="")
        trace_id = _as_str(obj.get("trace_id"), MAX_ID, default="")

        det_raw = obj.get("details")
        det: list[KV] = []
        if det_raw is not None:
            if not isinstance(det_raw, Sequence) or isinstance(det_raw, (str, bytes)):
                raise ValueError("error.details must be an array")
            for item in det_raw[:MAX_ERROR_DETAILS]:
                det.append(KV.from_obj(item))

        return ErrorBody(
            code=code,
            message=message,
            retryable=retryable,
            kind=kind,
            request_id=request_id,
            trace_id=trace_id,
            details=tuple(det),
        )


@dataclass(frozen=True)
class ErrorEnvelope:
    error: ErrorBody

    @staticmethod
    def from_obj(obj: Any) -> ErrorEnvelope:
        if not isinstance(obj, Mapping):
            raise ValueError("error envelope must be an object")
        err = obj.get("error")
        if not isinstance(err, Mapping):
            raise ValueError("missing 'error' object")
        return ErrorEnvelope(error=ErrorBody.from_obj(err))


def parse_error_envelope(obj: Any) -> ErrorEnvelope | None:
    """
    Best-effort parse for a Chartly error envelope.

    Returns None if shape does not match (caller can fallback to raw error handling).
    Raises ValueError only for obviously malformed 'error' shapes when present.
    """
    if not isinstance(obj, Mapping):
        return None
    if "error" not in obj:
        return None
    return ErrorEnvelope.from_obj(obj)
