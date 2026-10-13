"""Testes do contrato de triagem (offline — não dependem de LLM nem rede)."""

from muninn.models import (
    AlertNormalized,
    TriageDecision,
    Verdict,
    classify_heuristic,
)


def make_alert(raw: str, title: str = "alerta") -> AlertNormalized:
    return AlertNormalized(
        alert_id="a1",
        source="sentinel_one",
        title=title,
        severity="high",
        raw=raw,
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