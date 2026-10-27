"""Rotas e ciclo de vida da app: o worker sobe com a app e para no shutdown."""

from __future__ import annotations

import asyncio
from types import SimpleNamespace
from typing import Any

import pytest
from fastapi.testclient import TestClient

from muninn import app as app_module
from muninn.config.settings import Settings
from muninn.models import Category, TriageAssessment, Verdict
from muninn.worker import TriageWorker


class FakeWorker:
    def __init__(self) -> None:
        self.started = False
        self.stopped = False
        self.closed = False

    async def run(self, stop: asyncio.Event) -> None:
        self.started = True
        await stop.wait()
        self.stopped = True

    async def aclose(self) -> None:
        self.closed = True


class FakeAgent:
    """Agente que devolve uma avaliação fixa e registra o prompt recebido."""

    def __init__(self) -> None:
        self.prompts: list[str] = []

    async def run(self, prompt: str) -> Any:
        self.prompts.append(prompt)
        return SimpleNamespace(
            output=TriageAssessment(
                verdict=Verdict.TRUE_POSITIVE,
                confidence=0.93,
                category=Category.COMMAND_AND_CONTROL,
                rationale="Beacon para C2 conhecido.",
                recommended_action="Isolar o host.",
            )
        )


def use(monkeypatch: pytest.MonkeyPatch, **settings: Any) -> FakeWorker:
    fake = FakeWorker()
    # LLM desligado por padrão: os testes não tocam a rede.
    base: dict[str, Any] = {"litellm_base_url": ""}
    monkeypatch.setattr(app_module, "get_settings", lambda: Settings(**(base | settings)))
    monkeypatch.setattr(TriageWorker, "from_settings", lambda s: fake)
    return fake


def test_worker_runs_with_app_and_stops_on_shutdown(monkeypatch: pytest.MonkeyPatch) -> None:
    fake = use(monkeypatch, worker_enabled=True)
    with TestClient(app_module.app) as client:
        assert client.get("/healthz").json() == {"status": "ok"}
        assert fake.started
    assert fake.stopped and fake.closed


def test_worker_disabled(monkeypatch: pytest.MonkeyPatch) -> None:
    fake = use(monkeypatch, worker_enabled=False)
    with TestClient(app_module.app) as client:
        assert client.get("/healthz").status_code == 200
    assert not fake.started


def test_triage_route_falls_back_to_heuristic_without_llm(
    monkeypatch: pytest.MonkeyPatch, cef_event: dict[str, Any]
) -> None:
    use(monkeypatch, worker_enabled=False)
    with TestClient(app_module.app) as client:
        resp = client.post("/triage", json=cef_event)
    assert resp.status_code == 200
    body = resp.json()
    assert body["engine"] == "heuristic" and body["model"] and body["category"]


def test_triage_route_uses_the_configured_agent(
    monkeypatch: pytest.MonkeyPatch, cef_event: dict[str, Any]
) -> None:
    """A re-triagem sob demanda (ADR-002) passa pelo LLM, não pelo fallback."""
    agent = FakeAgent()
    use(monkeypatch, worker_enabled=False, litellm_base_url="http://litellm.test:4000")
    monkeypatch.setattr(app_module, "build_agent", lambda s: agent)
    with TestClient(app_module.app) as client:
        resp = client.post("/triage", json=cef_event)
    assert resp.status_code == 200
    body = resp.json()
    assert body["engine"] == "llm"
    assert body["verdict"] == "true_positive"
    # O prompt recebido cerca o evento como dado não confiável.
    assert agent.prompts and agent.prompts[0].startswith("Triage the security event")


def test_configure_logging_emits_muninn_info() -> None:
    import logging

    app_module.configure_logging("INFO")
    logger = logging.getLogger("muninn")
    assert logger.isEnabledFor(logging.INFO)
    assert logger.handlers
