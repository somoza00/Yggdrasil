"""Modelos de domínio da triagem. Saída estruturada (Pydantic) consumida por
ygg-api e huginn."""

from __future__ import annotations

from enum import StrEnum
from typing import Any, Literal, Self
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, field_validator

# Contrato de eventos (ADR-005). O muninn aceita qualquer MINOR da major 1.
SCHEMA_VERSION = "1.0"


class Verdict(StrEnum):
    """Resultado da triagem de um alerta."""

    TRUE_POSITIVE = "true_positive"
    FALSE_POSITIVE = "false_positive"
    REVIEW = "review"


class SeverityHint(StrEnum):
    """Severidade sugerida pela fonte (ADR-005). Dica, não decisão."""

    UNKNOWN = "unknown"
    INFO = "info"
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


class IocKind(StrEnum):
    """Tipos de IOC do contrato 1.x (ADR-005)."""

    IP = "ip"
    DOMAIN = "domain"
    HASH = "hash"
    URL = "url"


class Ioc(BaseModel):
    """Indicador extraído pelo huginn."""

    model_config = ConfigDict(extra="ignore")

    kind: IocKind
    value: str = Field(min_length=1)


class AlertNormalized(BaseModel):
    """NormalizedEvent do ADR-005 (huginn → events:ingest:normalized) — entrada
    da triagem. Schema: proto/events/normalized_event.schema.json.

    Forward-compat: campos desconhecidos são ignorados; valores de enum
    desconhecidos (adicionados numa MINOR futura) são tolerados — IOC de tipo
    desconhecido é descartado e severidade desconhecida vira `unknown`.
    """

    model_config = ConfigDict(extra="ignore")

    schema_version: str = Field(pattern=r"^1\.(0|[1-9][0-9]*)$")
    event_id: UUID
    source: str = Field(min_length=1, max_length=64)
    title: str = Field(min_length=1, max_length=256)
    severity_hint: SeverityHint
    raw: str
    parsed: dict[str, Any]
    iocs: list[Ioc]

    @field_validator("severity_hint", mode="before")
    @classmethod
    def _unknown_severity(cls, v: object) -> object:
        if isinstance(v, str) and v not in SeverityHint._value2member_map_:
            return SeverityHint.UNKNOWN
        return v

    @field_validator("iocs", mode="before")
    @classmethod
    def _drop_unknown_ioc_kinds(cls, v: object) -> object:
        if not isinstance(v, list):
            return v
        return [
            i
            for i in v
            if not (
                isinstance(i, dict)
                and isinstance(i.get("kind"), str)
                and i["kind"] not in IocKind._value2member_map_
            )
        ]


class Category(StrEnum):
    """Categoria da atividade identificada na triagem."""

    MALWARE = "malware"
    COMMAND_AND_CONTROL = "command_and_control"
    CREDENTIAL_ACCESS = "credential_access"
    EXFILTRATION = "exfiltration"
    LATERAL_MOVEMENT = "lateral_movement"
    RECONNAISSANCE = "reconnaissance"
    POLICY_VIOLATION = "policy_violation"
    OTHER = "other"


# Quem decidiu: o agente LLM ou o classificador determinístico de fallback.
Engine = Literal["llm", "heuristic"]

# Identificador versionado do classificador heurístico (campo `model`).
HEURISTIC_MODEL = "heuristic-v1"


class TriageAssessment(BaseModel):
    """Avaliação da triagem — exatamente o que o agente de IA deve produzir."""

    verdict: Verdict
    confidence: float = Field(ge=0.0, le=1.0)
    category: Category
    rationale: str
    recommended_action: str = ""


class TriageDecision(TriageAssessment):
    """Avaliação mais a procedência (engine/model), preenchida pelo muninn e
    nunca pelo LLM — para auditoria de quem tomou a decisão."""

    engine: Engine
    model: str = Field(min_length=1)


class TriageResult(BaseModel):
    """Payload do muninn para o ygg-api (POST /api/v1/incidents).
    Fixture do contrato: proto/triage/fixtures/triage_result.json."""

    event_id: UUID
    title: str
    source: str
    severity: SeverityHint
    iocs: list[Ioc]
    verdict: Verdict
    confidence: float = Field(ge=0.0, le=1.0)
    category: Category
    rationale: str
    recommended_action: str
    engine: Engine
    model: str
    # NormalizedEvent triado; o ygg-api guarda em incidents.raw_event.
    raw_event: dict[str, Any]

    @classmethod
    def from_triage(cls, alert: AlertNormalized, decision: TriageDecision) -> Self:
        return cls(
            event_id=alert.event_id,
            title=alert.title,
            source=alert.source,
            severity=alert.severity_hint,
            iocs=alert.iocs,
            raw_event=alert.model_dump(mode="json"),
            **decision.model_dump(),
        )


def classify_heuristic(alert: AlertNormalized) -> TriageDecision:
    """Classificador determinístico de fallback (sem LLM).

    Usado quando o gateway de LLM está indisponível ou em testes. NUNCA aplica
    IA de verdade; serve como rede de segurança e para validar o contrato de
    saída estruturada sem depender de rede.
    """
    text = f"{alert.title} {alert.raw}".lower()
    # Marcadores maliciosos têm precedência: o raw é controlado pelo atacante,
    # então um marcador de FP injetado não pode mascarar atividade maliciosa.
    if any(k in text for k in ("exfiltration", "ransomware", "powershell -enc", "cmd.exe /c")):
        return TriageDecision(
            verdict=Verdict.TRUE_POSITIVE,
            confidence=0.85,
            category=Category.EXFILTRATION if "exfiltration" in text else Category.MALWARE,
            rationale="Marcadores de atividade maliciosa presentes.",
            recommended_action="Escalar ao analista N2 e bloquear indicador.",
            engine="heuristic",
            model=HEURISTIC_MODEL,
        )
    if any(k in text for k in ("false positive", "known good", "whitelisted", "benign")):
        return TriageDecision(
            verdict=Verdict.FALSE_POSITIVE,
            confidence=0.8,
            category=Category.OTHER,
            rationale="Texto contém marcador explícito de falso positivo.",
            engine="heuristic",
            model=HEURISTIC_MODEL,
        )
    # Sem match: nunca assume FP — encaminha para revisão humana.
    return TriageDecision(
        verdict=Verdict.REVIEW,
        confidence=0.5,
        category=Category.OTHER,
        rationale="Sem marcadores fortes; requer revisão humana.",
        engine="heuristic",
        model=HEURISTIC_MODEL,
    )
