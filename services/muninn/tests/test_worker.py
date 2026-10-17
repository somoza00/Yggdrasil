"""Worker que consome events:ingest:normalized, tria e cria o incidente no
ygg-api. Redis e HTTP são falsos; nada de rede."""

from __future__ import annotations

import asyncio
import json
from typing import Any

import httpx
import pytest
from redis.exceptions import ConnectionError as RedisConnectionError
from redis.exceptions import ResponseError

from muninn.config.settings import Settings
from muninn.models import (
    AlertNormalized,
    Category,
    TriageDecision,
    TriageResult,
    Verdict,
)
from muninn.worker import READ_BLOCK_MS, Backoff, TriageWorker

STREAM = "events:ingest:normalized"

DECISION = TriageDecision(
    verdict=Verdict.TRUE_POSITIVE,
    confidence=0.9,
    category=Category.COMMAND_AND_CONTROL,
    rationale="Beacon to known C2.",
    recommended_action="Isolate host.",
    engine="llm",
    model="test-model",
)


async def fake_triage(alert: AlertNormalized) -> TriageDecision:
    return DECISION


class FakeRedis:
    """Responde xreadgroup com `batches` em ordem; esgotado, sinaliza parada."""

    def __init__(self, batches: list[Any] | None = None) -> None:
        self.batches = list(batches or [])
        self.reads: list[str] = []
        self.acked: list[str] = []
        self.groups: list[tuple[str, str, str, bool]] = []
        self.group_error: Exception | None = None
        self.stop = asyncio.Event()

    async def xgroup_create(
        self, name: str, groupname: str, id: str = "$", mkstream: bool = False
    ) -> bool:
        if self.group_error:
            raise self.group_error
        self.groups.append((name, groupname, id, mkstream))
        return True

    async def xreadgroup(
        self,
        groupname: str,
        consumername: str,
        streams: dict[str, str],
        count: int | None = None,
        block: int | None = None,
    ) -> Any:
        self.reads.append(streams[STREAM])
        if not self.batches:
            self.stop.set()
            return []
        item = self.batches.pop(0)
        if isinstance(item, Exception):
            raise item
        return item

    async def xack(self, name: str, groupname: str, *ids: str) -> int:
        self.acked.extend(ids)
        return len(ids)


class Recorder:
    def __init__(self, status: int = 201, error: Exception | None = None) -> None:
        self.status = status
        self.error = error
        self.requests: list[httpx.Request] = []

    def __call__(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        if self.error:
            raise self.error
        return httpx.Response(self.status, json={})


def make_worker(redis: FakeRedis, http: Recorder) -> TriageWorker:
    client = httpx.AsyncClient(base_url="http://ygg-api.test", transport=httpx.MockTransport(http))
    return TriageWorker(Settings(consumer_name="w1"), redis, client, fake_triage)


def entry(event: dict[str, Any], entry_id: str = "1-0") -> tuple[str, dict[str, str]]:
    return (entry_id, {"event": json.dumps(event)})


def test_valid_event_creates_incident_then_acks(cef_event: dict[str, Any]) -> None:
    redis, http = FakeRedis(), Recorder(201)
    ok = asyncio.run(make_worker(redis, http).process(*entry(cef_event)))
    assert ok and redis.acked == ["1-0"]
    (req,) = http.requests
    assert req.method == "POST" and req.url.path == "/api/v1/incidents"
    expected = TriageResult.from_triage(AlertNormalized.model_validate(cef_event), DECISION)
    assert json.loads(req.content) == expected.model_dump(mode="json")


def test_duplicate_delivery_is_acked(cef_event: dict[str, Any]) -> None:
    # ygg-api devolve 200 quando o event_id já virou incidente.
    redis = FakeRedis()
    assert asyncio.run(make_worker(redis, Recorder(200)).process(*entry(cef_event)))
    assert redis.acked == ["1-0"]


@pytest.mark.parametrize("status", [500, 503, 429])
def test_retryable_ygg_api_error_keeps_entry_pending(
    cef_event: dict[str, Any], status: int
) -> None:
    redis = FakeRedis()
    assert not asyncio.run(make_worker(redis, Recorder(status)).process(*entry(cef_event)))
    assert redis.acked == []


def test_ygg_api_unreachable_keeps_entry_pending(cef_event: dict[str, Any]) -> None:
    redis = FakeRedis()
    http = Recorder(error=httpx.ConnectError("refused"))
    assert not asyncio.run(make_worker(redis, http).process(*entry(cef_event)))
    assert redis.acked == []


@pytest.mark.parametrize("status", [401, 403])
def test_auth_rejection_keeps_entry_pending(cef_event: dict[str, Any], status: int) -> None:
    # Token errado é problema de configuração: descartar perderia o evento.
    redis = FakeRedis()
    assert not asyncio.run(make_worker(redis, Recorder(status)).process(*entry(cef_event)))
    assert redis.acked == []


def test_service_token_sent_as_bearer(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("YGG_API_SERVICE_TOKEN", "svc-token")
    worker = TriageWorker.from_settings(Settings(litellm_base_url=""))
    try:
        assert worker.http.headers["Authorization"] == "Bearer svc-token"
    finally:
        asyncio.run(worker.aclose())


def test_service_token_is_secret(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("YGG_API_SERVICE_TOKEN", "svc-token")
    settings = Settings()
    assert "svc-token" not in repr(settings)
    assert settings.ygg_api_service_token.get_secret_value() == "svc-token"


def test_missing_service_token_sends_no_header(
    monkeypatch: pytest.MonkeyPatch, caplog: pytest.LogCaptureFixture
) -> None:
    monkeypatch.delenv("YGG_API_SERVICE_TOKEN", raising=False)
    worker = TriageWorker.from_settings(Settings(litellm_base_url=""))
    try:
        assert "Authorization" not in worker.http.headers
        assert "YGG_API_SERVICE_TOKEN" in caplog.text
    finally:
        asyncio.run(worker.aclose())


def test_rejected_by_ygg_api_is_dropped(cef_event: dict[str, Any]) -> None:
    # 4xx = payload inválido: reprocessar não resolve e travaria o stream.
    redis = FakeRedis()
    assert asyncio.run(make_worker(redis, Recorder(400)).process(*entry(cef_event)))
    assert redis.acked == ["1-0"]


@pytest.mark.parametrize(
    "fields",
    [
        {"event": "{not json"},
        {"other": "x"},
        {"event": json.dumps({"schema_version": "2.0"})},
    ],
)
def test_invalid_event_is_acked_without_triage(fields: dict[str, str]) -> None:
    redis, http = FakeRedis(), Recorder()
    assert asyncio.run(make_worker(redis, http).process("9-0", fields))
    assert redis.acked == ["9-0"] and http.requests == []


def test_run_reads_own_pending_first_then_new(cef_event: dict[str, Any]) -> None:
    redis = FakeRedis([[[STREAM, [entry(cef_event)]]], []])
    worker = make_worker(redis, Recorder(201))
    asyncio.run(worker.run(redis.stop))
    assert redis.groups == [(STREAM, "muninn", "$", True)]
    # PEL ("0") com 1 entrada → PEL de novo, vazio → novas (">").
    assert redis.reads[:3] == ["0", "0", ">"]
    assert redis.acked == ["1-0"]


def test_run_rereads_pending_after_failure(cef_event: dict[str, Any]) -> None:
    redis = FakeRedis([[], [[STREAM, [entry(cef_event)]]]])
    asyncio.run(make_worker(redis, Recorder(503)).run(redis.stop))
    assert redis.reads == ["0", ">", "0"]
    assert redis.acked == []


def test_run_backs_off_on_redis_errors() -> None:
    redis = FakeRedis([RedisConnectionError("down"), []])
    asyncio.run(make_worker(redis, Recorder()).run(redis.stop))
    assert redis.reads == ["0", "0", ">"]


def test_existing_group_is_fine() -> None:
    redis = FakeRedis()
    redis.group_error = ResponseError("BUSYGROUP Consumer Group name already exists")
    asyncio.run(make_worker(redis, Recorder()).run(redis.stop))
    assert redis.reads  # seguiu para o loop de leitura


def test_backoff_doubles_from_100ms_to_30s() -> None:
    b = Backoff()
    delays = [b.next_delay() for _ in range(11)]
    assert delays == [0.1, 0.2, 0.4, 0.8, 1.6, 3.2, 6.4, 12.8, 25.6, 30.0, 30.0]
    b.reset()
    assert b.next_delay() == 0.1


def test_socket_timeout_exceeds_read_block() -> None:
    # Com o socket_timeout default do redis-py o cliente desistia antes do
    # BLOCK do XREADGROUP terminar: "Timeout reading from redis" a cada ciclo.
    worker = TriageWorker.from_settings(Settings(litellm_base_url=""))
    kwargs = worker.redis.connection_pool.connection_kwargs  # type: ignore[attr-defined]
    assert kwargs["socket_timeout"] > READ_BLOCK_MS / 1000
    asyncio.run(worker.aclose())


def test_run_recreates_group_after_nogroup() -> None:
    # Redis reiniciado sem persistência (ou FLUSHALL): o grupo some.
    redis = FakeRedis([ResponseError("NOGROUP No such key or consumer group"), []])
    asyncio.run(make_worker(redis, Recorder()).run(redis.stop))
    assert len(redis.groups) == 2


def test_repeated_processing_failures_back_off_exponentially(
    cef_event: dict[str, Any],
) -> None:
    # As leituras funcionam; quem falha é o ygg-api. O backoff não pode ser
    # zerado pela leitura bem-sucedida, senão o ygg-api é martelado a 100ms.
    pel = [[STREAM, [entry(cef_event)]]]
    redis = FakeRedis([[], pel, pel, pel])
    worker = make_worker(redis, Recorder(503))
    sleeps: list[float] = []

    async def record(stop: asyncio.Event, seconds: float) -> None:
        sleeps.append(seconds)

    worker._sleep = record
    asyncio.run(worker.run(redis.stop))
    assert sleeps == [0.1, 0.2, 0.4]
