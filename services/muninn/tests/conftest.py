"""Fixtures compartilhadas dos testes do muninn."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

PROTO_DIR = Path(__file__).resolve().parents[3] / "proto"


def load_proto(relative: str) -> dict[str, Any]:
    data: dict[str, Any] = json.loads((PROTO_DIR / relative).read_text())
    return data


@pytest.fixture
def cef_event() -> dict[str, Any]:
    """NormalizedEvent real produzido pelo huginn (ADR-005)."""
    return load_proto("events/fixtures/valid_cef.json")
