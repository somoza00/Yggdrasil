"""Agente de triagem de alertas com Pydantic AI.

O pydantic-ai é importado LAZY: se ele (ou o opentelemetry-api que ele exige)
não estiver disponível no ambiente, o muninn cai no classificador determinístico
de fallback em vez de quebrar na importação. Isso serve à camada de IA quando o
LLM estiver wireado e mantém a API viva sem ele.

NOTA de dependência conhecida: pydantic-ai>=0.8 importa
`opentelemetry._events`, módulo que só existe em opentelemetry-api>=1.46. Se o
índice do ambiente só tiver opentelemetry-api<=1.45.1, o agente LLM não carrega
(passa pelo fallback). Resolver = atualizar opentelemetry-api (ver ADR-004 spike).
"""

from __future__ import annotations

import logging
from typing import Any, cast

from muninn.config.settings import Settings
from muninn.models import AlertNormalized, TriageDecision, classify_heuristic

logger = logging.getLogger(__name__)

SYSTEM_PROMPT = (
    "You are a security analyst. Triage a security alert and respond strictly "
    "with the requested schema: verdict (true_positive | false_positive | "
    "review), confidence 0..1, rationale and recommended_action. Be concise and "
    "traceable (cite the indicators supporting the decision)."
)

def _build_agent(settings: Settings) -> Any:
    """Constrói o agente Pydantic AI. Só é chamado quando o LLM está em uso."""
    from pydantic_ai import Agent  # import lazy

    return Agent[None, TriageDecision](
        model=cast(Any, settings.model),
        output_type=TriageDecision,
        system_prompt=SYSTEM_PROMPT,
    )


async def triage(alert: AlertNormalized, settings: Settings | None = None) -> TriageDecision:
    """Roda a triagem. Tenta o LLM; em falha/indisponibilidade, usa o fallback."""
    settings = settings or Settings()
    try:
        agent = _build_agent(settings)
        result = await agent.run(alert.model_dump_json())
        return cast(TriageDecision, result.output)
    except Exception as exc:  # noqa: BLE001 — rede/LLM/deps podem falhar de várias formas
        logger.warning("LLM indisponível/erro, usando fallback: %s", exc)
        return classify_heuristic(alert)


def triage_sync(alert: AlertNormalized) -> TriageDecision:
    """Variante síncrona (rotas/testes) — sempre pelo fallback determinístico.
    Não faz chamada a LLM; garante resposta sem depender de rede nem do pydantic-ai."""
    return classify_heuristic(alert)