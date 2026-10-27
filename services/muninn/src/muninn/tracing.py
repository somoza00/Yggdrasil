"""Camada de observabilidade com Langfuse. Fica ativa quando creds presentes;
senão vira no-op para não quebrar dev/testes.

Uma trace por triagem, com o `event_id` como semente do `trace_id`: a mesma
entrada do stream (reentrega at-least-once) cai sempre no mesmo trace, então a
decisão fica rastreável a partir do incidente no ygg-api.
"""

from __future__ import annotations

import logging
from collections.abc import Iterator
from contextlib import contextmanager
from functools import cache
from typing import Any, Protocol

from muninn.config.settings import Settings
from muninn.models import AlertNormalized, TriageDecision

logger = logging.getLogger(__name__)


class Observation(Protocol):
    """Subconjunto da observation do Langfuse usado aqui."""

    def update(self, **kwargs: Any) -> Any: ...


def _build_client(s: Settings) -> Any:
    if not s.langfuse_public_key or not s.langfuse_secret_key:
        return None
    from langfuse import Langfuse  # import tardio: só exige o pacote se usado

    return Langfuse(
        host=s.langfuse_host,
        public_key=s.langfuse_public_key,
        secret_key=s.langfuse_secret_key,
    )


@cache
def _default_client() -> Any:
    """Cliente do ambiente, criado uma vez. Sem argumentos de propósito:
    Settings não é hashable e não pode ser chave de cache."""
    return _build_client(Settings())


def get_langfuse(settings: Settings | None = None) -> Any:
    """Retorna o cliente Langfuse, ou None se não configurado (no-op).
    Com `settings` explícito, cria um cliente novo (sem cache)."""
    if settings is None:
        return _default_client()
    return _build_client(settings)


def _open_triage(client: Any, alert: AlertNormalized) -> Any:
    """Abre a observation da triagem no cliente. `trace_id` derivado do
    `event_id` (seed determinística) para que a reentrega do mesmo evento
    atualize o mesmo trace em vez de criar outro."""
    return client.start_as_current_observation(
        as_type="agent",
        name="triage",
        trace_context={"trace_id": client.create_trace_id(seed=str(alert.event_id))},
        input={
            "event_id": str(alert.event_id),
            "source": alert.source,
            "title": alert.title,
            "severity_hint": alert.severity_hint.value,
        },
        metadata={"schema_version": alert.schema_version, "iocs": len(alert.iocs)},
    )


def finish_triage(span: Observation | None, decision: TriageDecision) -> None:
    """Grava a decisão na observation. Sem trace (`span` None) é no-op."""
    if span is None:
        return
    try:
        span.update(
            output={
                "verdict": decision.verdict.value,
                "confidence": decision.confidence,
                "category": decision.category.value,
                "rationale": decision.rationale,
                "recommended_action": decision.recommended_action,
            },
            metadata={"engine": decision.engine, "model": decision.model},
        )
    except Exception as exc:  # noqa: BLE001 — trace nunca derruba a triagem
        logger.warning("falha ao fechar a trace da triagem: %s", exc)


@contextmanager
def triage_span(settings: Settings, alert: AlertNormalized) -> Iterator[Observation | None]:
    """Trace da triagem, ou no-op (`yield None`) sem credenciais.

    Erro ao abrir a trace não interrompe a triagem: cai para no-op com um
    warning. Erro DENTRO do bloco sobe normal (a trace só observa).
    """
    client = get_langfuse(settings)
    if client is None:
        yield None
        return
    try:
        cm = _open_triage(client, alert)
    except Exception as exc:  # noqa: BLE001
        logger.warning("Langfuse indisponível, triagem segue sem trace: %s", exc)
        yield None
        return
    with cm as span:
        yield span
