"""Rotas e ciclo de vida da app: o worker sobe com a app e para no shutdown."""

from __future__ import annotations

import asyncio
from typing import Any

import pytest
from fastapi.testclient import TestClient

from muninn import app as app_module
from muninn.config.settings import Settings
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


def use(monkeypatch: pytest.MonkeyPatch, **settings: Any) -> FakeWorker:
    fake = FakeWorker()
    monkeypatch.setattr(app_module, "get_settings", lambda: Settings(**settings))
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


def test_sync_triage_route_reports_engine(
    monkeypatch: pytest.MonkeyPatch, cef_event: dict[str, Any]
) -> None:
    use(monkeypatch, worker_enabled=False)
    with TestClient(app_module.app) as client:
        resp = client.post("/triage", json=cef_event)
    assert resp.status_code == 200
    body = resp.json()
    assert body["engine"] == "heuristic" and body["model"] and body["category"]


def test_configure_logging_emits_muninn_info() -> None:
    import logging

    app_module.configure_logging("INFO")
    logger = logging.getLogger("muninn")
    assert logger.isEnabledFor(logging.INFO)
    assert logger.handlers
