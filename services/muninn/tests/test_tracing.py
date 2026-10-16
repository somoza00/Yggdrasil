"""Cliente Langfuse: no-op sem credenciais e cache só do cliente default."""

from __future__ import annotations

from collections.abc import Iterator
from typing import Any

import pytest

from muninn import tracing
from muninn.config.settings import Settings


@pytest.fixture(autouse=True)
def _clear_cache() -> Iterator[None]:
    tracing._default_client.cache_clear()
    yield
    tracing._default_client.cache_clear()


def test_accepts_explicit_settings_without_creds() -> None:
    # Settings não é hashable: com lru_cache keyed no argumento isto levantava TypeError.
    assert tracing.get_langfuse(Settings(langfuse_public_key="", langfuse_secret_key="")) is None


def test_default_client_is_built_once(monkeypatch: pytest.MonkeyPatch) -> None:
    calls: list[Settings] = []

    def fake_build(s: Settings) -> Any:
        calls.append(s)
        return object()

    monkeypatch.setattr(tracing, "_build_client", fake_build)
    first = tracing.get_langfuse()
    assert tracing.get_langfuse() is first
    assert len(calls) == 1


def test_explicit_settings_bypass_cache(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(tracing, "_build_client", lambda s: object())
    s = Settings()
    assert tracing.get_langfuse(s) is not tracing.get_langfuse(s)
