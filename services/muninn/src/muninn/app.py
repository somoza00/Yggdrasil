"""Aplicação FastAPI do muninn. Expõe o endpoint síncrono de triagem.

A decisão sobre gRPC vs REST vem do ADR-002; por enquanto expõe REST para
viabilizar a chamada síncrona — se o spike decidir gRPC, este contrato muda.
"""

from __future__ import annotations

from fastapi import FastAPI

from muninn.agents import triage_sync
from muninn.models import AlertNormalized, TriageDecision

app = FastAPI(title="muninn", version="0.1.0")


@app.post("/triage", response_model=TriageDecision)
def handle_triage(alert: AlertNormalized) -> TriageDecision:
    """Recebe um alerta normalizado e devolve a decisão estruturada."""
    return triage_sync(alert)


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}
