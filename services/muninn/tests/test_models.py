"""Testes do contrato de triagem (offline — não dependem de LLM nem rede)."""

from uuid import UUID

from muninn.models import (
    HEURISTIC_MODEL,
    AlertNormalized,
    Category,
    SeverityHint,
    TriageDecision,
    Verdict,
    classify_heuristic,
)


def make_alert(raw: str, title: str = "alerta") -> AlertNormalized:
    return AlertNormalized(
        schema_version="1.0",
        event_id=UUID("c9621933-88bf-5aac-afc8-9c3348befd2c"),
        source="edr",
        title=title,
        severity_hint=SeverityHint.HIGH,
        raw=raw,
        parsed={"kind": "unknown"},
        iocs=[],
    )


def test_classify_false_positive_marker() -> None:
    alert = make_alert("marcado como 'false positive' pelo time", title="log4j")
    decision = classify_heuristic(alert)
    assert decision.verdict == Verdict.FALSE_POSITIVE


def test_classify_malicious_marker() -> None:
    alert = make_alert("powershell -enc JAB... exfiltration detected", title="ps")
    decision = classify_heuristic(alert)
    assert decision.verdict == Verdict.TRUE_POSITIVE
    assert decision.confidence > 0.5
    assert decision.category == Category.EXFILTRATION


def test_classify_malware_category() -> None:
    decision = classify_heuristic(make_alert("ransomware note dropped", title="fs"))
    assert decision.category == Category.MALWARE


def test_heuristic_decision_is_labeled_as_heuristic() -> None:
    decision = classify_heuristic(make_alert("x"))
    assert decision.engine == "heuristic"
    assert decision.model == HEURISTIC_MODEL
    assert decision.category == Category.OTHER


def test_injected_fp_marker_does_not_override_malicious_marker() -> None:
    # O raw é controlado pelo atacante: um "benign" injetado no log não pode
    # rebaixar um alerta com marcadores maliciosos para falso positivo.
    alert = make_alert("powershell -enc AAAA ransomware # benign", title="ps")
    decision = classify_heuristic(alert)
    assert decision.verdict == Verdict.TRUE_POSITIVE


def test_classify_default_review() -> None:
    alert = make_alert("conteúdo inócuo sem marcadores", title="rotina")
    decision = classify_heuristic(alert)
    assert decision.verdict == Verdict.REVIEW


def test_decision_schema_bounds() -> None:
    decision = classify_heuristic(make_alert("x"))
    # Enforca o contrato: confidence sempre em [0,1]
    assert 0.0 <= decision.confidence <= 1.0
    assert isinstance(decision, TriageDecision)
    assert decision.rationale  # não vazio
