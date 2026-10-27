"""Cliente Langfuse: no-op sem credenciais e cache só do cliente default."""

from __future__ import annotations

from collections.abc import Iterator
from contextlib import contextmanager
from typing import Any

import pytest

from muninn import tracing
from muninn.config.settings import Settings
from muninn.models import AlertNormalized, classify_heuristic


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


class FakeObservation:
    def __init__(self) -> None:
        self.updates: list[dict[str, Any]] = []

    def update(self, **kwargs: Any) -> None:
        self.updates.append(kwargs)


class FakeClient:
    """Cliente mínimo: registra a seed do trace e os kwargs da observation."""

    def __init__(self) -> None:
        self.seeds: list[str | None] = []
        self.kwargs: dict[str, Any] = {}
        self.obs = FakeObservation()

    def create_trace_id(self, *, seed: str | None = None) -> str:
        self.seeds.append(seed)
        return f"trace-{seed}"

    @contextmanager
    def start_as_current_observation(self, **kwargs: Any) -> Iterator[FakeObservation]:
        self.kwargs = kwargs
        yield self.obs


def alert(cef_event: dict[str, Any]) -> AlertNormalized:
    return AlertNormalized.model_validate(cef_event)


def test_triage_span_is_noop_without_creds(cef_event: dict[str, Any]) -> None:
    s = Settings(langfuse_public_key="", langfuse_secret_key="")
    with tracing.triage_span(s, alert(cef_event)) as span:
        assert span is None


def test_triage_span_seeds_the_trace_with_the_event_id(
    monkeypatch: pytest.MonkeyPatch, cef_event: dict[str, Any]
) -> None:
    client = FakeClient()
    monkeypatch.setattr(tracing, "get_langfuse", lambda s: client)
    a = alert(cef_event)
    with tracing.triage_span(Settings(), a) as span:
        assert span is client.obs
    assert client.seeds == [str(a.event_id)]
    assert client.kwargs["trace_context"] == {"trace_id": f"trace-{a.event_id}"}
    assert client.kwargs["input"]["event_id"] == str(a.event_id)
    assert client.kwargs["as_type"] == "agent"


def test_finish_triage_records_the_decision(
    monkeypatch: pytest.MonkeyPatch, cef_event: dict[str, Any]
) -> None:
    client = FakeClient()
    monkeypatch.setattr(tracing, "get_langfuse", lambda s: client)
    a = alert(cef_event)
    with tracing.triage_span(Settings(), a) as span:
        tracing.finish_triage(span, classify_heuristic(a))
    output = client.obs.updates[0]
    assert output["output"]["verdict"] and output["output"]["rationale"]
    assert output["metadata"]["engine"] == "heuristic"


def test_finish_triage_without_span_is_noop(cef_event: dict[str, Any]) -> None:
    a = alert(cef_event)
    tracing.finish_triage(None, classify_heuristic(a))  # não levanta


def test_finish_triage_swallows_update_errors(cef_event: dict[str, Any]) -> None:
    """Falha ao gravar o output não pode derrubar a triagem já decidida."""

    class BrokenObservation:
        def update(self, **kwargs: Any) -> None:
            raise RuntimeError("trace não aceita mais updates")

    a = alert(cef_event)
    tracing.finish_triage(BrokenObservation(), classify_heuristic(a))  # não levanta


def test_triage_span_swallows_client_errors(
    monkeypatch: pytest.MonkeyPatch, cef_event: dict[str, Any]
) -> None:
    """Langfuse fora do ar não pode derrubar a triagem."""

    class Broken:
        def create_trace_id(self, *, seed: str | None = None) -> str:
            raise RuntimeError("langfuse fora do ar")

    monkeypatch.setattr(tracing, "get_langfuse", lambda s: Broken())
    with tracing.triage_span(Settings(), alert(cef_event)) as span:
        assert span is None
