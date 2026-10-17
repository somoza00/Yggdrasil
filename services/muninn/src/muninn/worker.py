"""Worker de triagem: consome events:ingest:normalized (ADR-005), tria cada
evento e cria o incidente no ygg-api (POST /api/v1/incidents).

Entrega at-least-once: o XACK só acontece depois que o ygg-api aceitou o
resultado. Duplicatas são resolvidas no ygg-api pelo event_id.
"""

from __future__ import annotations

import asyncio
import logging
from collections.abc import Awaitable, Callable
from contextlib import suppress
from functools import partial
from typing import Any, Protocol

import httpx
from pydantic import ValidationError
from redis.exceptions import RedisError, ResponseError

from muninn.agents.triage import build_agent, triage
from muninn.config.settings import Settings
from muninn.models import AlertNormalized, TriageDecision, TriageResult

logger = logging.getLogger(__name__)

READ_BLOCK_MS = 5000
# Timeout de socket: precisa exceder o BLOCK, senão o cliente desiste antes
# do servidor responder (o default do redis-py não excede).
SOCKET_TIMEOUT_S = READ_BLOCK_MS / 1000 + 5
READ_COUNT = 10
BACKOFF_START = 0.1
BACKOFF_MAX = 30.0

TriageFn = Callable[[AlertNormalized], Awaitable[TriageDecision]]


class StreamClient(Protocol):
    """Subconjunto do redis.asyncio.Redis usado pelo worker."""

    def xgroup_create(
        self, name: str, groupname: str, id: str = ..., mkstream: bool = ...
    ) -> Awaitable[Any]: ...

    def xreadgroup(
        self,
        groupname: str,
        consumername: str,
        streams: dict[Any, Any],
        count: int | None = ...,
        block: int | None = ...,
    ) -> Awaitable[Any]: ...

    def xack(self, name: str, groupname: str, *ids: str) -> Awaitable[Any]: ...


class Backoff:
    """Backoff exponencial: 100ms, dobra a cada falha, teto 30s."""

    def __init__(self) -> None:
        self._next = BACKOFF_START

    def next_delay(self) -> float:
        delay = self._next
        self._next = min(round(self._next * 2, 3), BACKOFF_MAX)
        return delay

    def reset(self) -> None:
        self._next = BACKOFF_START


def auth_headers(settings: Settings) -> dict[str, str]:
    """Authorization do POST /api/v1/incidents (ygg-api exige JWT)."""
    token = settings.ygg_api_service_token.get_secret_value()
    if not token:
        logger.warning("YGG_API_SERVICE_TOKEN vazio: o ygg-api vai recusar os incidentes (401)")
        return {}
    return {"Authorization": f"Bearer {token}"}


async def _sleep(stop: asyncio.Event, seconds: float) -> None:
    """Dorme, mas acorda na hora se o worker for parado."""
    try:
        await asyncio.wait_for(stop.wait(), timeout=seconds)
    except TimeoutError:
        pass


class TriageWorker:
    def __init__(
        self,
        settings: Settings,
        redis: StreamClient,
        http: httpx.AsyncClient,
        triage_fn: TriageFn,
    ) -> None:
        self.settings = settings
        self.redis = redis
        self.http = http
        self.triage_fn = triage_fn
        self.stream = settings.redis_stream_normalized
        self.group = settings.consumer_group
        self._sleep = _sleep

    @classmethod
    def from_settings(cls, settings: Settings) -> TriageWorker:
        from redis.asyncio import Redis

        # RESP2 explícito: o parsing do XREADGROUP abaixo assume a resposta em lista.
        redis = Redis.from_url(
            settings.redis_url,
            decode_responses=True,
            socket_connect_timeout=5,
            socket_timeout=SOCKET_TIMEOUT_S,
            protocol=2,
        )
        http = httpx.AsyncClient(
            base_url=settings.ygg_api_url,
            timeout=settings.ygg_api_timeout_seconds,
            headers=auth_headers(settings),
        )
        # Agente criado uma vez (reaproveita o cliente HTTP do LLM).
        triage_fn = partial(triage, settings=settings, agent=build_agent(settings))
        return cls(settings, redis, http, triage_fn)

    async def aclose(self) -> None:
        await self.http.aclose()
        close = getattr(self.redis, "aclose", None)
        if close is not None:
            await close()

    async def _ensure_group(self) -> None:
        try:
            await self.redis.xgroup_create(self.stream, self.group, id="$", mkstream=True)
            logger.info("grupo criado stream=%s group=%s", self.stream, self.group)
        except ResponseError as exc:
            if not str(exc).startswith("BUSYGROUP"):
                raise

    async def _ack(self, entry_id: str) -> None:
        try:
            await self.redis.xack(self.stream, self.group, entry_id)
        except RedisError as exc:
            # Fica no PEL; o reenvio é idempotente no ygg-api (event_id).
            logger.warning("falha no XACK de %s: %s", entry_id, exc)

    async def process(self, entry_id: str, fields: dict[str, str] | None) -> bool:
        """Processa uma entrada. True = resolvida (ACK feito); False = deve
        ser reprocessada (fica no PEL)."""
        payload = (fields or {}).get("event", "")
        try:
            alert = AlertNormalized.model_validate_json(payload)
        except ValidationError as exc:
            # ADR-005: evento fora do contrato não é processado. Dead-letter
            # stream é pendência; por ora vai para o log e sai da fila.
            logger.error("evento %s fora do contrato, descartado: %s", entry_id, exc)
            await self._ack(entry_id)
            return True

        decision = await self.triage_fn(alert)
        result = TriageResult.from_triage(alert, decision)
        try:
            resp = await self.http.post(
                "/api/v1/incidents",
                content=result.model_dump_json(),
                headers={"Content-Type": "application/json"},
            )
        except httpx.HTTPError as exc:
            logger.warning("ygg-api indisponível para %s: %s", alert.event_id, exc)
            return False
        if resp.status_code >= 500 or resp.status_code == 429:
            logger.warning("ygg-api respondeu %s para %s", resp.status_code, alert.event_id)
            return False
        if resp.status_code in (401, 403):
            # Token ausente, expirado ou de outro segredo: erro de configuração,
            # não do evento. Descartar perderia todos os eventos até a correção.
            logger.error(
                "ygg-api recusou o token de serviço (%s) para %s; evento fica pendente",
                resp.status_code,
                alert.event_id,
            )
            return False
        if resp.status_code >= 400:
            logger.error(
                "ygg-api rejeitou %s (%s): %s", alert.event_id, resp.status_code, resp.text
            )
            await self._ack(entry_id)
            return True

        await self._ack(entry_id)
        logger.info(
            "evento %s triado verdict=%s engine=%s model=%s",
            alert.event_id,
            decision.verdict,
            decision.engine,
            decision.model,
        )
        return True

    async def run(self, stop: asyncio.Event) -> None:
        """Loop de consumo até `stop`. Começa pelo próprio PEL (id "0") e só
        depois lê mensagens novas (">"); qualquer falha volta ao PEL.

        Dois backoffs independentes: o de leitura zera a cada XREADGROUP ok; o
        de processamento só zera quando um evento é resolvido. Com um só, a
        leitura bem-sucedida zeraria a espera e o ygg-api fora do ar seria
        chamado a cada 100ms."""
        read_backoff, process_backoff = Backoff(), Backoff()
        while not stop.is_set():
            try:
                await self._ensure_group()
                break
            except RedisError as exc:
                delay = read_backoff.next_delay()
                logger.warning("falha ao criar grupo, nova tentativa em %ss: %s", delay, exc)
                await self._sleep(stop, delay)

        read_backoff.reset()
        read_id = "0"
        consumer = self.settings.consumer_name
        logger.info(
            "worker consumindo stream=%s group=%s consumer=%s", self.stream, self.group, consumer
        )
        while not stop.is_set():
            try:
                reply = await self.redis.xreadgroup(
                    self.group,
                    consumer,
                    {self.stream: read_id},
                    count=READ_COUNT,
                    block=READ_BLOCK_MS,
                )
            except RedisError as exc:
                if str(exc).startswith("NOGROUP"):
                    # Redis perdeu os dados (restart sem persistência, FLUSHALL).
                    with suppress(RedisError):
                        await self._ensure_group()
                delay = read_backoff.next_delay()
                logger.warning("falha no XREADGROUP, nova tentativa em %ss: %s", delay, exc)
                await self._sleep(stop, delay)
                continue
            read_backoff.reset()

            entries = [e for _, stream_entries in reply or [] for e in stream_entries]
            if read_id == "0" and not entries:
                read_id = ">"  # PEL vazio: segue para mensagens novas
                continue

            for entry_id, fields in entries:
                if not await self.process(entry_id, fields):
                    read_id = "0"
                    await self._sleep(stop, process_backoff.next_delay())
                    break
                process_backoff.reset()
