"""
Chartly 2.0  Python SDK (v0)

This package is intentionally thin: it provides consistent HTTP calling patterns,
header conventions (tenant/request id), tracing propagation (W3C trace-context),
and structured error decoding.

PEP 561 typing support:
- Include an empty `py.typed` file alongside this package to publish type hints.
  (Added as a separate build target in this project.)
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Any

__version__ = "0.1.0"

__all__ = ["__version__", "Client"]

if TYPE_CHECKING:
    # Static typing only; avoids import-time dependency on .client
    from .client import Client as Client  # noqa: F401


def __getattr__(name: str) -> Any:
    """
    Lazy attribute resolver.

    This avoids exposing Client=None (confusing runtime errors) and keeps imports fast.
    """
    if name == "Client":
        try:
            from .client import Client  # type: ignore
        except ImportError as e:
            raise ImportError(
                "chartly.Client is not available because chartly.client could not be imported. "
                "If you are using the SDK package, ensure it is installed correctly "
                "(e.g., `pip install chartly-sdk`)."
            ) from e
        return Client
    raise AttributeError(f"module 'chartly' has no attribute {name!r}")
