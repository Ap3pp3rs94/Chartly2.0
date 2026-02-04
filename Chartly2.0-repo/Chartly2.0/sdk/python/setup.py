from __future__ import annotations

import os
from pathlib import Path

from setuptools import find_packages, setup


def _read_readme() -> str:
    here = Path(__file__).resolve().parent
    readme = here / "README.md"
    if readme.exists():
        return readme.read_text(encoding="utf-8")
    return "Chartly 2.0  Python SDK (v0). Thin HTTP client helpers for Chartly services."


HERE = Path(__file__).resolve().parent

# Support both `src/` layout and flat layout without guessing package names.
HAS_SRC_LAYOUT = (HERE / "src").is_dir()

PKG_KWARGS = {}
if HAS_SRC_LAYOUT:
    PKG_KWARGS["package_dir"] = {"": "src"}
    PKG_KWARGS["packages"] = find_packages(where="src")
else:
    PKG_KWARGS["packages"] = find_packages()


setup(
    name="chartly-sdk",
    version="0.1.0",
    description="Chartly 2.0 Python SDK (thin HTTP helpers; contracts-first platform)",
    long_description=_read_readme(),
    long_description_content_type="text/markdown",
    author="Chartly 2.0",
    license="MIT",
    python_requires=">=3.10",
    install_requires=[
        "requests>=2.31.0",
    ],
    extras_require={
        "dev": [
            "pytest>=7.0.0",
            "ruff>=0.5.0",
            "mypy>=1.10.0",
            "types-requests>=2.31.0.0",
        ],
    },
    include_package_data=True,
    zip_safe=False,
    classifiers=[
        "Development Status :: 3 - Alpha",
        "Intended Audience :: Developers",
        "License :: OSI Approved :: MIT License",
        "Programming Language :: Python :: 3",
        "Programming Language :: Python :: 3 :: Only",
        "Programming Language :: Python :: 3.10",
        "Programming Language :: Python :: 3.11",
        "Programming Language :: Python :: 3.12",
        "Topic :: Software Development :: Libraries",
        "Topic :: System :: Networking",
    ],
    **PKG_KWARGS,
)
