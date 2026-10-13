"""Modelos de domínio da triagem. Saída estruturada (Pydantic) consumida por
ygg-api e huginn."""

from __future__ import annotations

from enum import StrEnum

from pydantic import BaseModel, Field


class Verdict(StrEnum):
    """Resultado da triagem de um alerta."""

    TRUE_POSITIVE = "true_positive"
    FALSE_POSITIVE = "false_positive"
    REVIEW = "review"


class AlertNormalized(BaseModel):
    """Alerta normalizado vindo do pipeline (huginn) — entrada da triagem."""

    alert_id: str
    source: str  # edr | siem | custom
    title: str
    severity: str
    raw: str
    iocs: list[str] = Field(default_factory=list)
    tags: list[str] = Field(default_factory=list)


class TriageDecision(BaseModel):
    """Decisão estruturada da triagem — o que o agente de IA deve produzir."""

    verdict: Verdict
    confidence: float = Field(ge=0.0, le=1.0)
    rationale: str
    recommended_action: str = ""


def classify_heuristic(alert: AlertNormalized) -> TriageDecision:
    """Classificador determinístico de fallback (sem LLM).

    Usado quando o gateway de LLM está indisponível ou em testes. NUNCA aplica
    IA de verdade; serve como rede de segurança e para validar o contrato de
    saída estruturada sem depender de rede.
    """
    text = f"{alert.title} {alert.raw}".lower()
    # Heurística simples: marcadores óbvios de falso positivo.
    if any(k in text for k in ("false positive", "known good", "whitelisted", "benign")):
        return TriageDecision(
            verdict=Verdict.FALSE_POSITIVE,
            confidence=0.8,
            rationale="Texto contém marcador explícito de falso positivo.",
        )
    if any(k in text for k in ("exfiltration", "ransomware", "powershell -enc", "cmd.exe /c")):
        return TriageDecision(
            verdict=Verdict.TRUE_POSITIVE,
            confidence=0.85,
            rationale="Marcadores de atividade maliciosa presentes.",
            recommended_action="Escalar ao analista N2 e bloquear indicador.",
        )
    return TriageDecision(
        verdict=Verdict.REVIEW,
        confidence=0.5,
        rationale="Sem marcadores fortes; requer revisão humana.",
    )