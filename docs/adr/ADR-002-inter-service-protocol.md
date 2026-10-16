# ADR-002 — Protocolo síncrono entre serviços (gRPC vs REST)

- **Status:** FECHADO — **REST**, duas chamadas síncronas
- **Data:** 2026-10-13 (rascunho inicial) · 2026-10-16 (decisão)
- **Relacionados:** ADR-005 (contrato assíncrono via Redis Streams)

## Contexto

Os três serviços (ygg-api / huginn / muninn) precisam se comunicar de forma
síncrona em pontos específicos, com latência baixa e contratos claros:

- ygg-api (Go) → muninn (Python): pede uma triagem IA de um alerta.
- muninn (Python) → ygg-api (Go): registra/atualiza decisão e incidente.
- huginn (Rust) → muninn (Python): envia evento normalizado para enriquecimento
  por IA / recebe enriquecimento.
- ygg-api (Go) → huginn (Rust): consulta estado de um evento/correlação.

Fluxo pesado (streams de eventos) é assíncrono via Redis Streams (fora do escopo
deste ADR). Este ADR trata só da **chamada síncrona request/response** de baixo
volume relativo (triagens, consultas de incidente) — mas com requisito de baixa
latência e saída estruturada.

## Opções

### gRPC (proto3)
- **Prós** (na arquitetura polyglot): schemas versionados em `.proto`
  independem de linguagem; HTTP/2 com multiplex e baixa latência; codegen
  eficiente para Go e Rust; Python suportado (grpcio).
- **Contras**: tooling (grpclib), curva de aprendizado, debuggability pior
  (binário por default — mitigável com reflection/gateway), e o Python é o elo
  mais fraco do ecossistema gRPC.

### REST (OpenAPI)
- **Prós**: ubíquo, fácil de debugar/curl, tooling maduro em Python (FastAPI
  gera OpenAPI nativo), Go tem ótimo suporte.
- **Contras**: schemas versionados ficam como spec externa (snake em go vs
  snake/camel cross-language), tipagem compartilhada mais solta, latência e
  eficiência menores que HTTP/2 binário (na prática irrelevante para este volume).

## Critérios de decisão (para o spike medir)

1. Latência p99 na chamada de triagem (ygg-api→muninn).
2. Esforço de codegen e manutenção de contrato em Go+Rust+Python.
3. Facilidade de debug em dev.
4. Suporte a tooling de observabilidade (trace IDs atravessando).
5. Riscos de dependency hell em Python (grpcio vs fastapi/clients).

## Decisão

**REST (HTTP + JSON).** O spike gRPC vs REST não foi executado: a decisão foi
tomada antes dele, porque o volume síncrono é baixo e o fluxo principal já é
assíncrono (ADR-005). Existem **exatamente duas** chamadas síncronas:

| # | Chamada | Quando | Contrato |
|---|---|---|---|
| 1 | ygg-api → muninn `POST /triage` | Re-triagem sob demanda (exposta pelo ygg-api em `POST /api/v1/triage`) | Corpo: `NormalizedEvent` (ADR-005). Resposta: `TriageDecision` |
| 2 | muninn → ygg-api `POST /api/v1/incidents` | Depois da triagem assíncrona de cada evento do stream `events:ingest:normalized` | Corpo: `TriageResult`, fixture em `proto/triage/fixtures/triage_result.json`. Idempotente por `event_id` (201 cria, 200 já existia) |

As demais interações do Contexto não são síncronas:

- huginn → muninn passa pelo Redis Streams (ADR-005), não por chamada direta.
- ygg-api → huginn (consulta de estado) não existe. Se for necessária, exige
  revisar este ADR.

Regras para as duas chamadas:

- Os consumidores ignoram campos desconhecidos (mesma regra do ADR-005).
- Timeouts explícitos: o cliente muninn no ygg-api usa 60 s (cobre o LLM); o
  cliente ygg-api no muninn usa `MUNNIN_YGG_API_TIMEOUT_SECONDS` (10 s).
- Na chamada 2, um 5xx, 429 ou erro de rede mantém o evento pendente no stream
  para nova tentativa. Um 4xx descarta o evento com log de erro.

## Consequências

- Debug com `curl` e o OpenAPI gerado pelo FastAPI no muninn. Não há codegen
  nem `.proto`: `proto/` guarda só os schemas e fixtures de contrato.
- O contrato da chamada 2 é verificado pela fixture compartilhada nos testes do
  muninn e do ygg-api. Ainda falta uma spec OpenAPI do ygg-api.
- Pendente: autenticação serviço a serviço. Hoje as duas rotas aceitam
  qualquer chamada que chegue pela rede.