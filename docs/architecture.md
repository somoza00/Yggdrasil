# Arquitetura Yggdrasil — visão geral

> Fonte de verdade de decisões: `docs/adr/`. Este doc é a visão de conjunto.

## Visão de alto nível

```
              ┌──────────────────────────────────────────────────────┐
  fontes  ──▶ │  huginn (Rust)   ingest/parse/enrich/correlate       │
 (EDR, SIEM,   │  Redis Streams · CEF / syslog · IOC · regras         │
  syslog,     └───────────────┬──────────────────────────────────────┘
  CEF, webh)                   │ eventos normalizados
                               ▼
              ┌──────────────────────────────────────────────────────┐
              │  muninn (Python)  agentes de IA de triagem          │
              │  Pydantic AI · LiteLLM · Langfuse                   │
              └───────────────┬──────────────────────────────────────┘
                               │ decisão (TP / FP / review) + rationale
                               ▼
              ┌──────────────────────────────────────────────────────┐
              │  ygg-api (Go)  API gateway                           │
              │  JWT auth · incidentes · playbooks · Postgres        │
              └───────────────┬──────────────────────────────────────┘
                               │ notification / ticketing / actions
```

## Serviços

### huginn (Rust) — ingestão e correlação
- Consome Redis Streams (consumer group) de fontes.
- Parsing CEF (Common Event Format) e syslog.
- Enriquecimento de IOCs (endereço, domínio, hash, CVE...).
- Regras de correlação (agregação, thresholds).

### muninn (Python) — camada de IA
- Agente de triagem com **Pydantic AI**: entrada = alerta normalizado;
  saída típica:
  ```json
  { "verdict": "true_positive|false_positive|review",
    "confidence": 0.92,
    "rationale": "...",
    "recommended_action": "..." }
  ```
- **LiteLLM** gateway para LLMs (vários provedores).
- **Langfuse** para traces/auditoria das decisões.

### ygg-api (Go) — API e orquestração
- Auth JWT (ADR-006).
- Recursos: incidentes, run history.
- Incidentes: listar, criar (idempotente por `event_id`), buscar, revisar veredito
  (`PATCH .../verdict`, auditado) e medir a acurácia da triagem
  (`GET /api/v1/incidents/stats/triage-accuracy`): compara o veredito original
  da IA com o veredito final do analista, com recortes por engine/model e por
  veredito proposto.
- Dispara triagens via contrato síncrono com muninn (ver ADR-002).
- Persistência em Postgres.

## Storage
- **PostgreSQL** — incidentes, playbooks, usuários, run history (migrations versionadas).
- **Redis** — streams de ingest (fila) + cache de enriquecimento e sessões.

## Deploy
- Kubernetes + kustomize: `deploy/kustomize/base` (3 serviços + ingress) e
  `overlays/{dev,prod}`.
- Segredos K8s / OpenBao em produção; `.env` apenas local.

## Fluxo de contrato síncrono (ADR-002)
**REST, com exatamente duas chamadas:** ygg-api → muninn `POST /triage`
(re-triagem sob demanda, com o agente LLM e fallback heurístico) e muninn →
ygg-api `POST /api/v1/incidents` (após a triagem assíncrona; idempotente por
`event_id`). Todo o resto passa pelo Redis Streams (ADR-005). Uma chamada
síncrona nova exige revisar o ADR-002.
