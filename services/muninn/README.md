# muninn

Camada de IA da Yggdrasil: triagem de alertas com **Pydantic AI**, gateway
LLM via **LiteLLM**, observabilidade com **Langfuse**.

Ver `docs/adr/ADR-004-ai-layer.md` para a decisão de arquitetura.

## Desenvolvimento (uv)

```sh
uv sync --extra dev
uv run pytest          # testes (offline, sem LLM)
uv run ruff check .
uv run ruff format .
uv run mypy src
uv run uvicorn muninn.app:app --port 8090   # sobe a API
```

## Contrato

`POST /triage` — recebe `AlertNormalized`, devolve `TriageDecision`
(estrutura em `src/muninn/models.py`). A decisão passa pelo agente (LLM via
LiteLLM) e, se o LLM estiver desligado ou falhar, pelo classificador
heurístico de fallback; `engine` diz quem decidiu (`llm` | `heuristic`).

Com credenciais de Langfuse configuradas, cada triagem grava uma trace usando o
`event_id` do alerta como `trace_id` — a decisão fica rastreável a partir do
incidente no ygg-api. Sem credenciais a trace é no-op.

> Nota: o endpoint é REST por ora. Se o ADR-002 decidir gRPC, este contrato
> muda para proto.