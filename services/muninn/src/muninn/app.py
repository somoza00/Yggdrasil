"""Aplicação FastAPI do muninn.

- POST /triage: chamada síncrona do ygg-api (REST, ADR-002). Usa o agente LLM e,
  se ele estiver desligado ou falhar, o classificador heurístico de fallback.
- Worker em background: consome events:ingest:normalized (ADR-005), tria via
  LLM e cria o incidente no ygg-api. Desligável com MUNNIN_WORKER_ENABLED=false.
"""

from __future__ import annotations

import asyncio
import logging
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager, suppress

from fastapi import FastAPI, Request

from muninn.agents.triage import build_agent, triage
from muninn.config.settings import get_settings
from muninn.models import AlertNormalized, TriageDecision
from muninn.worker import TriageWorker

logger = logging.getLogger(__name__)

# Tempo para o worker terminar o evento corrente no shutdown.
SHUTDOWN_GRACE_SECONDS = 10


def configure_logging(level: str) -> None:
    """Logs do pacote muninn no stderr. O uvicorn só configura os loggers dele;
    sem isto, INFO do worker (evento triado, engine usada) não aparece."""
    logger = logging.getLogger("muninn")
    logger.setLevel(level.upper())
    if not logger.handlers:
        handler = logging.StreamHandler()
        handler.setFormatter(logging.Formatter("%(asctime)s %(levelname)s %(name)s: %(message)s"))
        logger.addHandler(handler)


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    settings = get_settings()
    configure_logging(settings.log_level)
    app.state.settings = settings
    # Agente criado uma vez por processo: reusa o cliente HTTP do LLM no
    # endpoint e no worker. None quando o LLM está desligado.
    app.state.triage_agent = build_agent(settings)
    if not settings.worker_enabled:
        yield
        return
    worker = TriageWorker.from_settings(settings)
    stop = asyncio.Event()
    task = asyncio.create_task(worker.run(stop), name="triage-worker")
    try:
        yield
    finally:
        stop.set()
        try:
            await asyncio.wait_for(task, timeout=SHUTDOWN_GRACE_SECONDS)
        except TimeoutError:
            task.cancel()
            with suppress(asyncio.CancelledError):
                await task
        except Exception:
            logger.exception("worker terminou com erro")
        await worker.aclose()


app = FastAPI(title="muninn", version="0.1.0", lifespan=lifespan)


@app.post("/triage", response_model=TriageDecision)
async def handle_triage(alert: AlertNormalized, request: Request) -> TriageDecision:
    """Recebe um alerta normalizado e devolve a decisão estruturada.

    Caminho LLM com fallback heurístico: nunca devolve erro por o LLM estar
    fora. A decisão diz qual engine decidiu (`decision.engine`).
    """
    settings = request.app.state.settings
    return await triage(alert, settings=settings, agent=request.app.state.triage_agent)


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}
